// Package auth hosts the HTTP handlers for /api/v1/auth/*.
package auth

import (
	intauth "github.com/karnstack/tempo/internal/auth"
	"github.com/labstack/echo/v4"
	"go.uber.org/zap"
)

// Configure mounts the auth route group. Every route here is public —
// session-required middleware lives on protected groups (0038+).
func Configure(e *echo.Echo, _ *zap.Logger, m *intauth.Manager, r *intauth.Registrar, a *intauth.Authenticator) {
	g := e.Group("/api/v1/auth")
	g.GET("/firstrun", firstRunHandler(r))
	g.POST("/register", registerHandler(m, r))
	g.POST("/login", loginHandler(a, m))
	g.POST("/logout", logoutHandler(m))
}
