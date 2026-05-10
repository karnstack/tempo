package auth

import (
	"errors"
	"net/http"

	"github.com/karnstack/tempo/internal/api/web"
	intauth "github.com/karnstack/tempo/internal/auth"
	"github.com/labstack/echo/v4"
	"go.uber.org/zap"
)

type LoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type LoginResponse struct {
	User web.UserDTO `json:"user"`
}

func loginHandler(a *intauth.Authenticator, m *intauth.Manager) echo.HandlerFunc {
	return web.WrapPublic(func(ctx *web.Context) error {
		var req LoginRequest
		if err := ctx.Bind(&req); err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "invalid request body")
		}

		user, err := a.Authenticate(ctx.Request().Context(), req.Email, req.Password)
		switch {
		case errors.Is(err, intauth.ErrInvalidCredentials):
			return echo.NewHTTPError(http.StatusUnauthorized, "invalid credentials")
		case err != nil:
			ctx.L.Error("login failed", zap.Error(err))
			return echo.NewHTTPError(http.StatusInternalServerError, "login failed")
		}

		if _, err := m.Issue(ctx.Request().Context(), ctx.Response(), user.ID); err != nil {
			ctx.L.Error("issue session failed", zap.Error(err))
			return echo.NewHTTPError(http.StatusInternalServerError, "session issue failed")
		}
		return ctx.JSON(http.StatusOK, LoginResponse{User: web.UserDTO{
			ID:    user.ID,
			Email: user.Email,
			Role:  user.Role,
		}})
	})
}
