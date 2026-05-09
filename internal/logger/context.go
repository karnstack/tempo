package logger

import (
	"context"

	"go.uber.org/zap"
)

type ctxKey struct{}

// IntoContext returns a copy of ctx carrying l. FromContext on the result
// returns l. Use this when starting a request, scheduling a job, or otherwise
// minting a context that should propagate a scoped logger.
func IntoContext(ctx context.Context, l *zap.Logger) context.Context {
	return context.WithValue(ctx, ctxKey{}, l)
}

// FromContext returns the logger previously attached via IntoContext, or a
// no-op logger if none is present. Callers never need a nil check.
func FromContext(ctx context.Context) *zap.Logger {
	if l, ok := ctx.Value(ctxKey{}).(*zap.Logger); ok && l != nil {
		return l
	}
	return zap.NewNop()
}
