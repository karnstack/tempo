package main

import (
	"github.com/karnstack/tempo/internal/api"
	"github.com/karnstack/tempo/internal/logger"
	"go.uber.org/fx"
	"go.uber.org/fx/fxevent"
	"go.uber.org/zap"
)

func main() {
	fx.New(
		fx.Provide(logger.New),
		fx.Decorate(func(l *zap.Logger) *zap.Logger {
			return l.With(zap.String("service", "tempo"))
		}),
		fx.Invoke(api.Run),
		fx.WithLogger(func(l *zap.Logger) fxevent.Logger {
			return &fxevent.ZapLogger{Logger: l}
		}),
	).Run()
}
