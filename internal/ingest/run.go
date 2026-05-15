package ingest

import (
	"context"

	"github.com/karnstack/tempo/internal/config"
	"github.com/karnstack/tempo/internal/secret"
	"github.com/karnstack/tempo/internal/storage/sqlite/sqlitedb"
	"go.uber.org/fx"
	"go.uber.org/zap"
)

// RunParams collects every fx-injected dependency of the ingest scheduler.
// Runners are pulled from the "ingest.runners" value group, populated by
// per-resource ingest packages (0027–0030).
type RunParams struct {
	fx.In

	Lifecycle fx.Lifecycle
	Logger    *zap.Logger
	Config    *config.Config
	Queries   *sqlitedb.Queries
	Box       *secret.Box
	Runners   []Runner `group:"ingest.runners"`
}

// Run is the fx entrypoint for the ingest worker. It builds the Scheduler
// and hooks its goroutine into the lifecycle.
func Run(p RunParams) error {
	s := New(p.Logger, p.Config, p.Queries, p.Box, p.Runners)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})

	p.Lifecycle.Append(fx.Hook{
		OnStart: func(_ context.Context) error {
			go func() {
				defer close(done)
				s.Loop(ctx)
			}()
			p.Logger.Info("ingest scheduler started",
				zap.Duration("interval", p.Config.Poll.Interval),
				zap.Int("runners", len(p.Runners)),
			)
			return nil
		},
		OnStop: func(stopCtx context.Context) error {
			p.Logger.Info("ingest scheduler stopping")
			cancel()
			select {
			case <-done:
				return nil
			case <-stopCtx.Done():
				return stopCtx.Err()
			}
		},
	})
	return nil
}
