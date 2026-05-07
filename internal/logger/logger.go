// Package logger builds the application zap logger and wires its lifecycle into fx.
package logger

import (
	"context"
	"errors"
	"syscall"

	"github.com/karnstack/tempo/internal/config"
	"go.uber.org/fx"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func New(lc fx.Lifecycle) (*zap.Logger, error) {
	cfg := zap.NewProductionConfig()
	if config.IsDev() {
		cfg = zap.NewDevelopmentConfig()
		cfg.EncoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
	}

	l, err := cfg.Build()
	if err != nil {
		return nil, err
	}

	lc.Append(fx.Hook{
		OnStop: func(_ context.Context) error {
			if err := l.Sync(); err != nil &&
				!errors.Is(err, syscall.ENOTTY) &&
				!errors.Is(err, syscall.EINVAL) &&
				!errors.Is(err, syscall.EBADF) {
				return err
			}
			return nil
		},
	})

	return l, nil
}

// NewStandalone builds a zap logger without an fx lifecycle. Use it for CLI tools
// and one-shot binaries that don't run inside fx.
func NewStandalone() *zap.Logger {
	cfg := zap.NewProductionConfig()
	if config.IsDev() {
		cfg = zap.NewDevelopmentConfig()
		cfg.EncoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
	}
	l, err := cfg.Build()
	if err != nil {
		panic(err)
	}
	return l
}
