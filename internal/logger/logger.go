// Package logger builds the application zap logger and wires its lifecycle into fx.
package logger

import (
	"context"
	"errors"
	"fmt"
	"syscall"

	"github.com/karnstack/tempo/internal/config"
	"go.uber.org/fx"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// New returns a zap logger configured from cfg.Log. Level must be one of
// debug|info|warn|error; Format must be json or console. Console format
// adds colored capital level labels for human-friendly dev output.
func New(lc fx.Lifecycle, cfg *config.Config) (*zap.Logger, error) {
	lvl, err := zapcore.ParseLevel(cfg.Log.Level)
	if err != nil {
		return nil, fmt.Errorf("logger: parse TEMPO_LOG_LEVEL=%q: %w", cfg.Log.Level, err)
	}

	zc := zap.NewProductionConfig()
	if cfg.Log.Format == "console" {
		zc = zap.NewDevelopmentConfig()
		zc.EncoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
	}
	zc.Encoding = cfg.Log.Format
	zc.Level = zap.NewAtomicLevelAt(lvl)

	l, err := zc.Build()
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

// NewStandalone builds a zap logger without an fx lifecycle. Use it for CLI
// tools and one-shot binaries that boot before config is loaded.
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
