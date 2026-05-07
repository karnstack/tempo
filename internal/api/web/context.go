// Package web provides the request context and handler wrappers used by tempo's API.
package web

import (
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
func WrapPublic(h HandlerFunc, l *zap.Logger) echo.HandlerFunc {
	return func(c echo.Context) error {
		rid := c.Response().Header().Get(echo.HeaderXRequestID)
		ctx := &Context{
			Context: c,
			L:       l.With(zap.String("request_id", rid)),
		}
		return h(ctx)
	}
}
