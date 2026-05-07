// Package api hosts the echo server and route registration for tempo's REST API.
package api

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/karnstack/tempo/internal/api/health"
	"github.com/karnstack/tempo/internal/config"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"go.uber.org/fx"
	"go.uber.org/zap"
)

func Run(lc fx.Lifecycle, l *zap.Logger) error {
	cfg := config.Load()

	e := echo.New()
	if !config.IsDev() {
		e.HideBanner = true
		e.HidePort = true
	}

	configureMiddleware(e, l)
	configureRoutes(e, l)

	server := &http.Server{
		Addr:              cfg.Listen,
		Handler:           e,
		ReadTimeout:       30 * time.Second,
		ReadHeaderTimeout: 5 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}

	lc.Append(fx.Hook{
		OnStart: func(_ context.Context) error {
			go func() {
				l.Info("starting tempo api", zap.String("addr", server.Addr))
				if err := e.StartServer(server); err != nil && !errors.Is(err, http.ErrServerClosed) {
					l.Error("error starting echo server", zap.Error(err))
				}
			}()
			return nil
		},
		OnStop: func(ctx context.Context) error {
			l.Info("shutdown signal received")
			return e.Shutdown(ctx)
		},
	})

	return nil
}

func configureMiddleware(e *echo.Echo, l *zap.Logger) {
	e.Use(middleware.RequestID())
	e.Use(middleware.RecoverWithConfig(middleware.RecoverConfig{
		StackSize: 1 << 12,
		LogErrorFunc: func(c echo.Context, err error, stack []byte) error {
			l.Error("recovered from panic",
				zap.Error(err),
				zap.ByteString("stack", stack),
				zap.String("request_id", c.Response().Header().Get(echo.HeaderXRequestID)),
			)
			return nil
		},
	}))
}

func configureRoutes(e *echo.Echo, l *zap.Logger) {
	e.GET("/", func(c echo.Context) error {
		return c.String(http.StatusOK, "Hello, tempo")
	})
	health.Configure(e, l)
}
