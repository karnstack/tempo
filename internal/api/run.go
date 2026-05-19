// Package api hosts the echo server and route registration for tempo's REST API.
package api

import (
	"context"
	"errors"
	"net/http"
	"time"

	apiauth "github.com/karnstack/tempo/internal/api/auth"
	"github.com/karnstack/tempo/internal/api/connections"
	"github.com/karnstack/tempo/internal/api/health"
	"github.com/karnstack/tempo/internal/api/me"
	"github.com/karnstack/tempo/internal/api/tokens"
	intauth "github.com/karnstack/tempo/internal/auth"
	"github.com/karnstack/tempo/internal/config"
	"github.com/karnstack/tempo/internal/secret"
	"github.com/karnstack/tempo/internal/storage/sqlite/sqlitedb"
	"github.com/karnstack/tempo/internal/webui"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"go.uber.org/fx"
	"go.uber.org/zap"
)

func Run(lc fx.Lifecycle, l *zap.Logger, cfg *config.Config, m *intauth.Manager, r *intauth.Registrar, a *intauth.Authenticator, q *sqlitedb.Queries, box *secret.Box) error {
	e := echo.New()
	if cfg.Env != "development" {
		e.HideBanner = true
		e.HidePort = true
	}

	configureMiddleware(e, l)
	configureRoutes(e, l, m, r, a, q, box, cfg)

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
	e.Use(requestLogger(l))
	e.Use(middleware.RecoverWithConfig(middleware.RecoverConfig{
		StackSize: 1 << 12,
		LogErrorFunc: func(c echo.Context, err error, stack []byte) error {
			l.Error("recovered from panic",
				zap.Error(err),
				zap.ByteString("stack", stack),
				zap.String("trace_id", c.Response().Header().Get(echo.HeaderXRequestID)),
			)
			return nil
		},
	}))
}

func configureRoutes(e *echo.Echo, l *zap.Logger, m *intauth.Manager, r *intauth.Registrar, a *intauth.Authenticator, q *sqlitedb.Queries, box *secret.Box, cfg *config.Config) {
	health.Configure(e, l)
	apiauth.Configure(e, l, m, r, a)
	me.Configure(e, l, m, q)
	tokens.Configure(e, l, m, q, box)
	connections.Configure(e, l, m, q, cfg)

	// SPA fallback — must be last so /api/* routes win.
	e.GET("/*", echo.WrapHandler(webui.Handler()))
}
