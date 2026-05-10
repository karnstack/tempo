package auth

import (
	"net/http"

	"github.com/karnstack/tempo/internal/api/web"
	intauth "github.com/karnstack/tempo/internal/auth"
	"github.com/labstack/echo/v4"
	"go.uber.org/zap"
)

func logoutHandler(m *intauth.Manager) echo.HandlerFunc {
	return web.WrapPublic(func(ctx *web.Context) error {
		if err := m.Revoke(ctx.Request().Context(), ctx.Response(), ctx.Request()); err != nil {
			ctx.L.Error("revoke session failed", zap.Error(err))
			return echo.NewHTTPError(http.StatusInternalServerError, "logout failed")
		}
		return ctx.NoContent(http.StatusNoContent)
	})
}
