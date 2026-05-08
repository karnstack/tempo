package main

import (
	"context"
	"time"

	"github.com/karnstack/tempo/internal/api"
	"github.com/karnstack/tempo/internal/config"
	"github.com/karnstack/tempo/internal/logger"
	"github.com/karnstack/tempo/internal/storage"
	"github.com/karnstack/tempo/internal/storage/sqlite"
	"go.uber.org/fx"
	"go.uber.org/fx/fxevent"
	"go.uber.org/zap"
)

func main() {
	fx.New(
		fx.Provide(
			logger.New,
			config.Load,
			sqlite.New,
		),
		fx.Decorate(func(l *zap.Logger) *zap.Logger {
			return l.With(zap.String("service", "tempo"))
		}),
		fx.Invoke(func(cfg *config.Config, l *zap.Logger) {
			if cfg.SecretWarning != "" {
				l.Warn(cfg.SecretWarning)
			}
		}),
		fx.Invoke(api.Run),
		fx.Invoke(touchStorage),
		fx.WithLogger(func(l *zap.Logger) fxevent.Logger {
			return &fxevent.ZapLogger{Logger: l}
		}),
	).Run()
}

// touchStorage forces fx to instantiate the Storage so the SQLite open + PRAGMA
// checks run at boot. Real consumers (auth, ingest, rollup) replace this in 0016+.
func touchStorage(s storage.Storage, l *zap.Logger) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := s.Ping(ctx); err != nil {
		return err
	}
	l.Info("storage warmup ok")
	return nil
}
