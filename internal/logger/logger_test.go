package logger

import (
	"context"
	"testing"

	"github.com/karnstack/tempo/internal/config"
	"go.uber.org/fx"
	"go.uber.org/fx/fxtest"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func TestNew_HonorsLevel(t *testing.T) {
	cases := []struct {
		level string
		want  zapcore.Level
	}{
		{"debug", zapcore.DebugLevel},
		{"info", zapcore.InfoLevel},
		{"warn", zapcore.WarnLevel},
		{"error", zapcore.ErrorLevel},
	}
	for _, tc := range cases {
		t.Run(tc.level, func(t *testing.T) {
			lc := fxtest.NewLifecycle(t)
			l, err := New(lc, &config.Config{Log: config.Log{Level: tc.level, Format: "json"}})
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			core := l.Core()
			if !core.Enabled(tc.want) {
				t.Errorf("level %s: want %s enabled", tc.level, tc.want)
			}
			// Anything below want must be disabled.
			if tc.want > zapcore.DebugLevel && core.Enabled(tc.want-1) {
				t.Errorf("level %s: %s should be disabled", tc.level, tc.want-1)
			}
		})
	}
}

func TestNew_HonorsFormat(t *testing.T) {
	for _, format := range []string{"json", "console"} {
		t.Run(format, func(t *testing.T) {
			lc := fxtest.NewLifecycle(t)
			l, err := New(lc, &config.Config{Log: config.Log{Level: "info", Format: format}})
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			if l == nil {
				t.Fatal("logger is nil")
			}
		})
	}
}

func TestNew_InvalidLevelErrors(t *testing.T) {
	// fxtest.Lifecycle doesn't tolerate the empty fx.Lifecycle here, but New
	// returns its parse error before touching lc, so a real lifecycle isn't
	// necessary. Use a real one anyway to keep the test honest.
	lc := fxtest.NewLifecycle(t)
	_, err := New(lc, &config.Config{Log: config.Log{Level: "trace", Format: "json"}})
	if err == nil {
		t.Fatal("expected error for invalid level, got nil")
	}
}

func TestNew_LifecycleSyncsOnStop(t *testing.T) {
	app := fxtest.New(t,
		fx.Supply(&config.Config{Log: config.Log{Level: "info", Format: "json"}}),
		fx.Provide(New),
		fx.Invoke(func(*zap.Logger) {}),
	)
	app.RequireStart().RequireStop()
}

func TestContextRoundTrip(t *testing.T) {
	want := zap.NewNop().With(zap.String("k", "v"))
	ctx := IntoContext(context.Background(), want)
	got := FromContext(ctx)
	if got != want {
		t.Errorf("FromContext returned a different logger than was attached")
	}
}

func TestFromContext_DefaultIsUsableNop(t *testing.T) {
	l := FromContext(context.Background())
	if l == nil {
		t.Fatal("FromContext returned nil")
	}
	// Must not panic.
	l.Info("ok")
	l.Error("ok")
	l.With(zap.String("x", "y")).Debug("ok")
}

func TestFromContext_NilLoggerStoredFallsBack(t *testing.T) {
	ctx := context.WithValue(context.Background(), ctxKey{}, (*zap.Logger)(nil))
	l := FromContext(ctx)
	if l == nil {
		t.Fatal("FromContext returned nil for explicitly-nil stored logger")
	}
	l.Info("ok") // must not panic
}
