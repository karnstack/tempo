package auth

import (
	"net/http"

	"github.com/karnstack/tempo/internal/api/web"
	intauth "github.com/karnstack/tempo/internal/auth"
	"github.com/labstack/echo/v4"
	"go.uber.org/zap"
)

type FirstRunResponse struct {
	FirstRun bool `json:"first_run"`
}

func firstRunHandler(r *intauth.Registrar) echo.HandlerFunc {
	return web.WrapPublic(func(ctx *web.Context) error {
		first, err := r.IsFirstRun(ctx.Request().Context())
		if err != nil {
			ctx.L.Error("first-run check failed", zap.Error(err))
			return echo.NewHTTPError(http.StatusInternalServerError, "first-run check failed")
		}
		return ctx.JSON(http.StatusOK, FirstRunResponse{FirstRun: first})
	})
}
