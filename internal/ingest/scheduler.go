package ingest

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/karnstack/tempo/internal/config"
	"github.com/karnstack/tempo/internal/github"
	"github.com/karnstack/tempo/internal/secret"
	"github.com/karnstack/tempo/internal/storage/sqlite/sqlitedb"
	"go.uber.org/zap"
)

// ClientBuilder constructs a *github.Client for a single connection's PAT.
// Injected so tests can substitute a client pointed at a fake transport.
type ClientBuilder func(token string, l *zap.Logger) *github.Client

func defaultClientBuilder(token string, l *zap.Logger) *github.Client {
	return github.New(token, github.WithLogger(l))
}

// Scheduler walks active connections on every tick, dispatches them to the
// registered runners, and records the outcome on the sync_runs table.
type Scheduler struct {
	log       *zap.Logger
	cfg       *config.Config
	q         *sqlitedb.Queries
	box       *secret.Box
	runners   []Runner
	newClient ClientBuilder
	now       func() time.Time
}

// Option mutates a Scheduler after New. Used by tests to inject fakes.
type Option func(*Scheduler)

// WithClientBuilder overrides the per-connection GitHub client builder.
func WithClientBuilder(b ClientBuilder) Option { return func(s *Scheduler) { s.newClient = b } }

// WithClock overrides time.Now. Useful for deterministic sync_run timestamps.
func WithClock(now func() time.Time) Option { return func(s *Scheduler) { s.now = now } }

// New builds a Scheduler with production defaults.
func New(l *zap.Logger, cfg *config.Config, q *sqlitedb.Queries, box *secret.Box, runners []Runner, opts ...Option) *Scheduler {
	s := &Scheduler{
		log:       l,
		cfg:       cfg,
		q:         q,
		box:       box,
		runners:   runners,
		newClient: defaultClientBuilder,
		now:       time.Now,
	}
	for _, o := range opts {
		o(s)
	}
	return s
}

// Loop runs Tick at boot, then on every interval tick until ctx is cancelled.
func (s *Scheduler) Loop(ctx context.Context) {
	s.Tick(ctx)
	if ctx.Err() != nil {
		return
	}
	t := time.NewTicker(s.cfg.Poll.Interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.Tick(ctx)
		}
	}
}

// Tick lists active connections and syncs each in turn. Errors on individual
// connections are recorded onto their sync_runs row but never abort the tick.
func (s *Scheduler) Tick(ctx context.Context) {
	if ctx.Err() != nil {
		return
	}
	conns, err := s.q.ListActiveConnections(ctx)
	if err != nil {
		s.log.Error("ingest: list active connections", zap.Error(err))
		return
	}
	for _, conn := range conns {
		if ctx.Err() != nil {
			return
		}
		s.syncConnection(ctx, conn)
	}
}

// syncConnection drives one connection through one tick.
func (s *Scheduler) syncConnection(ctx context.Context, conn sqlitedb.Connection) {
	startedAt := s.now().UTC()
	run, err := s.q.StartSyncRun(ctx, sqlitedb.StartSyncRunParams{
		ConnectionID: conn.ID,
		StartedAt:    startedAt,
	})
	if err != nil {
		s.log.Error("ingest: start sync_run",
			zap.Int64("connection_id", conn.ID), zap.Error(err))
		return
	}

	cl, err := s.buildClient(ctx, conn)
	if err != nil {
		s.finishRun(ctx, run.ID, 0, 0, nil, err.Error())
		return
	}

	var (
		totalItems int64
		minRemain  *int64
	)
	for _, r := range s.runners {
		if ctx.Err() != nil {
			s.finishRun(ctx, run.ID, 0, totalItems, minRemain, ctx.Err().Error())
			return
		}
		out, rerr := r.Run(ctx, conn, cl)
		totalItems += out.Items
		minRemain = minRemaining(minRemain, out.RateLimitRemaining)
		if rerr != nil {
			msg := fmt.Sprintf("%s: %s", r.Name(), rerr.Error())
			s.log.Warn("ingest: runner failed",
				zap.Int64("connection_id", conn.ID),
				zap.String("runner", r.Name()),
				zap.Error(rerr),
			)
			s.finishRun(ctx, run.ID, 0, totalItems, minRemain, msg)
			return
		}
	}

	s.finishRun(ctx, run.ID, 1, totalItems, minRemain, "")
	if err := s.q.UpdateConnectionLastSync(ctx, sqlitedb.UpdateConnectionLastSyncParams{
		LastSyncAt: &startedAt,
		ID:         conn.ID,
	}); err != nil {
		s.log.Error("ingest: update last_sync_at",
			zap.Int64("connection_id", conn.ID), zap.Error(err))
	}
}

// buildClient resolves the PAT for a connection and constructs a per-token
// *github.Client. The PAT is decrypted in-process; the plaintext never
// leaves this function.
func (s *Scheduler) buildClient(ctx context.Context, conn sqlitedb.Connection) (*github.Client, error) {
	tok, err := s.q.GetGhToken(ctx, conn.TokenID)
	if err != nil {
		return nil, fmt.Errorf("token lookup: %w", err)
	}
	pat, err := s.box.Decrypt(tok.EncryptedPat)
	if err != nil {
		return nil, fmt.Errorf("token decrypt: %w", err)
	}
	cl := s.newClient(string(pat), s.log.With(
		zap.Int64("connection_id", conn.ID),
		zap.String("connection_kind", conn.Kind),
	))
	return cl, nil
}

// finishRun closes a sync_runs row. Uses context.Background() when ctx is
// cancelled so the final write still lands.
func (s *Scheduler) finishRun(ctx context.Context, runID int64, ok, items int64, remaining *int64, errMsg string) {
	finishedAt := s.now().UTC()
	writeCtx := ctx
	if errors.Is(ctx.Err(), context.Canceled) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
		var cancel context.CancelFunc
		writeCtx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
	}
	if err := s.q.FinishSyncRun(writeCtx, sqlitedb.FinishSyncRunParams{
		FinishedAt:         &finishedAt,
		Ok:                 ok,
		Items:              items,
		RateLimitRemaining: remaining,
		Error:              errMsg,
		ID:                 runID,
	}); err != nil {
		s.log.Error("ingest: finish sync_run",
			zap.Int64("sync_run_id", runID), zap.Error(err))
	}
}

// minRemaining returns the smaller of two optional remaining counts, where
// nil is treated as "not observed". The min represents the worst-case
// rate-limit headroom seen during a tick.
func minRemaining(a, b *int64) *int64 {
	switch {
	case a == nil:
		return b
	case b == nil:
		return a
	case *b < *a:
		return b
	default:
		return a
	}
}
