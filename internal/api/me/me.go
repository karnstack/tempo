// Package me hosts the GET /api/v1/me handler — the smallest authenticated
// endpoint, used by the SPA to bootstrap the current-user state.
package me

import (
	"database/sql"
	"errors"
	"net/http"

	"github.com/karnstack/tempo/internal/api/web"
	intauth "github.com/karnstack/tempo/internal/auth"
	"github.com/karnstack/tempo/internal/storage/sqlite/sqlitedb"
	"github.com/labstack/echo/v4"
	"go.uber.org/zap"
)

// MeResponse is the wire shape returned by GET /me.
type MeResponse struct {
	User web.UserDTO `json:"user"`
}

// Configure mounts GET /api/v1/me behind RequireSession.
func Configure(e *echo.Echo, _ *zap.Logger, m *intauth.Manager, q *sqlitedb.Queries) {
	g := e.Group("/api/v1", web.RequireSession(m))
	g.GET("/me", meHandler(m, q))
}

func meHandler(m *intauth.Manager, q *sqlitedb.Queries) echo.HandlerFunc {
	return web.WrapPublic(func(ctx *web.Context) error {
		sess, ok := intauth.FromContext(ctx.Request().Context())
		if !ok {
			return echo.NewHTTPError(http.StatusUnauthorized, "unauthorized")
		}
		user, err := q.GetUser(ctx.Request().Context(), sess.UserID)
		if errors.Is(err, sql.ErrNoRows) {
			_ = m.Revoke(ctx.Request().Context(), ctx.Response(), ctx.Request())
			return echo.NewHTTPError(http.StatusUnauthorized, "unauthorized")
		}
		if err != nil {
			ctx.L.Error("get user failed", zap.Error(err))
			return echo.NewHTTPError(http.StatusInternalServerError, "lookup failed")
		}
		return ctx.JSON(http.StatusOK, MeResponse{User: web.UserDTO{
			ID:    user.ID,
			Email: user.Email,
			Role:  user.Role,
		}})
	})
}
