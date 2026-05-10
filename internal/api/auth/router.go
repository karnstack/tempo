// Package auth hosts the HTTP handlers for /api/v1/auth/*.
package auth

import (
	intauth "github.com/karnstack/tempo/internal/auth"
	"github.com/labstack/echo/v4"
	"go.uber.org/zap"
)

// Configure mounts the auth route group. /firstrun and /register are
// both public — login middleware lives on protected groups (0038+).
func Configure(e *echo.Echo, _ *zap.Logger, m *intauth.Manager, r *intauth.Registrar) {
	g := e.Group("/api/v1/auth")
	g.GET("/firstrun", firstRunHandler(r))
	g.POST("/register", registerHandler(m, r))
}
