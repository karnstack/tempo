// Package web provides the request context and handler wrappers used by tempo's API.
package web

import (
	"github.com/karnstack/tempo/internal/logger"
	"github.com/labstack/echo/v4"
	"go.uber.org/zap"
)

// Context wraps echo.Context with the request-scoped logger. Auth and tenant
// fields will be added when the auth tasks land.
type Context struct {
	echo.Context
	L *zap.Logger
}

type HandlerFunc func(ctx *Context) error

// WrapPublic wraps a handler for endpoints that do not require authentication.
// The request-scoped logger is pulled from c.Request().Context(); the api
// requestLogger middleware places it there.
func WrapPublic(h HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		return h(&Context{
			Context: c,
			L:       logger.FromContext(c.Request().Context()),
		})
	}
}
