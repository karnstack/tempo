package github

import (
	"context"
	"sync"
	"time"
)

// Limiter pauses callers when GitHub says the bucket is nearly empty. State
// is updated from response headers; until the first Update, Wait is a no-op
// (we have no information yet, so we proceed and let the first response
// teach us).
type Limiter struct {
	mu        sync.Mutex
	remaining int
	resetAt   time.Time
	floor     int
	now       func() time.Time
	sleep     func(context.Context, time.Duration) error
}

// NewLimiter returns a Limiter with floor=200, real wall clock, and a
// ctx-aware sleep.
func NewLimiter() *Limiter {
	return &Limiter{
		remaining: -1,
		floor:     200,
		now:       time.Now,
		sleep:     ctxSleep,
	}
}

// Wait blocks if remaining < floor, until the bucket resets or ctx is
// cancelled.
func (l *Limiter) Wait(ctx context.Context) error {
	l.mu.Lock()
	if l.remaining < 0 || l.remaining >= l.floor {
		l.mu.Unlock()
		return nil
	}
	until := l.resetAt
	l.mu.Unlock()
	d := until.Sub(l.now())
	if d <= 0 {
		return nil
	}
	return l.sleep(ctx, d)
}

// Update is called after every API call with the bucket state from response
// headers. Negative remaining is treated as "unknown" and clears state.
func (l *Limiter) Update(remaining int, resetAt time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.remaining = remaining
	l.resetAt = resetAt
}

func ctxSleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
