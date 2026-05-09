package api

import (
	"time"

	"github.com/karnstack/tempo/internal/logger"
	"github.com/labstack/echo/v4"
	"go.uber.org/zap"
)

// requestLogger builds a per-request *zap.Logger tagged with request_id,
// injects it into c.Request().Context() so downstream code can pull it via
// logger.FromContext, and emits one structured access log entry after the
// handler returns. Severity scales with response status; handler errors are
// always logged at error level. Must be registered after middleware.RequestID
// so the ID is already attached to the response header.
func requestLogger(l *zap.Logger) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			start := time.Now()
			req := c.Request()

			rid := req.Header.Get(echo.HeaderXRequestID)
			if rid == "" {
				rid = c.Response().Header().Get(echo.HeaderXRequestID)
			}
			rl := l.With(zap.String("request_id", rid))
			c.SetRequest(req.WithContext(logger.IntoContext(req.Context(), rl)))

			err := next(c)
			// Trigger echo's HTTPErrorHandler now so res.Status reflects the
			// final response by the time we log. Subsequent calls (from
			// Echo.ServeHTTP) are idempotent once the response is committed.
			if err != nil {
				c.Error(err)
			}

			res := c.Response()
			fields := []zap.Field{
				zap.String("method", req.Method),
				zap.String("path", req.URL.Path),
				zap.Int("status", res.Status),
				zap.Int64("bytes", res.Size),
				zap.Int64("latency_ms", time.Since(start).Milliseconds()),
				zap.String("ip", c.RealIP()),
			}
			if err != nil {
				fields = append(fields, zap.Error(err))
			}
			switch {
			case res.Status >= 500:
				rl.Error("request", fields...)
			case res.Status >= 400:
				rl.Warn("request", fields...)
			default:
				rl.Info("request", fields...)
			}
			return err
		}
	}
}
