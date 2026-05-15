package github

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestLimiterWaitNoopWhenUnknown(t *testing.T) {
	l := NewLimiter()
	if err := l.Wait(context.Background()); err != nil {
		t.Fatalf("Wait unknown: %v", err)
	}
}

func TestLimiterWaitNoopWhenAboveFloor(t *testing.T) {
	l := NewLimiter()
	l.Update(500, time.Now().Add(time.Minute))
	if err := l.Wait(context.Background()); err != nil {
		t.Fatalf("Wait above floor: %v", err)
	}
}

func TestLimiterWaitBlocksUntilReset(t *testing.T) {
	now := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
	var slept time.Duration
	l := &Limiter{
		floor: 200,
		now:   func() time.Time { return now },
		sleep: func(_ context.Context, d time.Duration) error { slept = d; return nil },
	}
	l.Update(10, now.Add(2*time.Second))
	if err := l.Wait(context.Background()); err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if slept != 2*time.Second {
		t.Fatalf("slept = %v, want 2s", slept)
	}
}

func TestLimiterWaitNoopWhenResetInPast(t *testing.T) {
	now := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
	called := false
	l := &Limiter{
		floor: 200,
		now:   func() time.Time { return now },
		sleep: func(context.Context, time.Duration) error { called = true; return nil },
	}
	l.Update(0, now.Add(-1*time.Second))
	if err := l.Wait(context.Background()); err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if called {
		t.Fatal("sleep called even though reset is in the past")
	}
}

func TestLimiterWaitCancellable(t *testing.T) {
	l := &Limiter{
		floor: 200,
		now:   time.Now,
		sleep: func(ctx context.Context, _ time.Duration) error { return ctx.Err() },
	}
	l.Update(0, time.Now().Add(time.Hour))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := l.Wait(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Wait err = %v, want Canceled", err)
	}
}

func TestLimiterRemainingUnknownBeforeUpdate(t *testing.T) {
	l := NewLimiter()
	if n, ok := l.Remaining(); ok {
		t.Errorf("Remaining() = (%d, %v), want (_, false) before any Update", n, ok)
	}
}

func TestLimiterRemainingAfterUpdate(t *testing.T) {
	l := NewLimiter()
	l.Update(4321, time.Now().Add(time.Minute))
	n, ok := l.Remaining()
	if !ok {
		t.Fatal("Remaining() ok = false, want true after Update")
	}
	if n != 4321 {
		t.Errorf("Remaining() n = %d, want 4321", n)
	}
}

func TestLimiterUpdateRace(t *testing.T) {
	l := NewLimiter()
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(2)
		go func() { defer wg.Done(); l.Update(1, time.Now()) }()
		go func() { defer wg.Done(); _ = l.Wait(context.Background()) }()
	}
	wg.Wait()
}
