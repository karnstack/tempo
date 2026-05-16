package rollup

import (
	"context"

	"github.com/karnstack/tempo/internal/config"
	"github.com/karnstack/tempo/internal/storage/sqlite/sqlitedb"
	"go.uber.org/fx"
	"go.uber.org/zap"
)

// RunParams collects every fx-injected dependency of the rollup
// scheduler. Aggregators are pulled from the "rollup.aggregators"
// value group, populated by per-slice rollup packages (0033–0036).
type RunParams struct {
	fx.In

	Lifecycle   fx.Lifecycle
	Logger      *zap.Logger
	Config      *config.Config
	Queries     *sqlitedb.Queries
	Aggregators []Aggregator `group:"rollup.aggregators"`
}

// Run is the fx entrypoint for the rollup worker. It builds the
// Scheduler and hooks its goroutine into the lifecycle.
func Run(p RunParams) error {
	s := New(p.Logger, p.Config, p.Queries, p.Aggregators)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})

	p.Lifecycle.Append(fx.Hook{
		OnStart: func(_ context.Context) error {
			go func() {
				defer close(done)
				s.Loop(ctx)
			}()
			tz := "local"
			if p.Config.Rollup.Timezone != nil {
				tz = p.Config.Rollup.Timezone.String()
			}
			p.Logger.Info("rollup scheduler started",
				zap.Int("hour", p.Config.Rollup.Hour),
				zap.String("tz", tz),
				zap.Int("aggregators", len(p.Aggregators)),
			)
			return nil
		},
		OnStop: func(stopCtx context.Context) error {
			p.Logger.Info("rollup scheduler stopping")
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
