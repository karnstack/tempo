package auth

import (
	"errors"
	"net/http"

	"github.com/karnstack/tempo/internal/api/web"
	intauth "github.com/karnstack/tempo/internal/auth"
	"github.com/labstack/echo/v4"
	"go.uber.org/zap"
)

type RegisterRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type RegisterResponse struct {
	User UserDTO `json:"user"`
}

type UserDTO struct {
	ID    int64  `json:"id"`
	Email string `json:"email"`
	Role  string `json:"role"`
}

func registerHandler(m *intauth.Manager, r *intauth.Registrar) echo.HandlerFunc {
	return web.WrapPublic(func(ctx *web.Context) error {
		var req RegisterRequest
		if err := ctx.Bind(&req); err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "invalid request body")
		}

		user, err := r.Register(ctx.Request().Context(), req.Email, req.Password)
		switch {
		case errors.Is(err, intauth.ErrInvalidEmail):
			return echo.NewHTTPError(http.StatusBadRequest, "invalid email")
		case errors.Is(err, intauth.ErrPasswordTooShort):
			return echo.NewHTTPError(http.StatusBadRequest, "password must be at least 8 characters")
		case errors.Is(err, intauth.ErrNotFirstRun):
			return echo.NewHTTPError(http.StatusConflict, "registration is closed")
		case err != nil:
			ctx.L.Error("register failed", zap.Error(err))
			return echo.NewHTTPError(http.StatusInternalServerError, "register failed")
		}

		if _, err := m.Issue(ctx.Request().Context(), ctx.Response(), user.ID); err != nil {
			ctx.L.Error("issue session failed", zap.Error(err))
			return echo.NewHTTPError(http.StatusInternalServerError, "session issue failed")
		}

		return ctx.JSON(http.StatusCreated, RegisterResponse{User: UserDTO{
			ID:    user.ID,
			Email: user.Email,
			Role:  user.Role,
		}})
	})
}
