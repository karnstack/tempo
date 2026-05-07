package health

import (
	"github.com/karnstack/tempo/internal/api/web"
	"github.com/labstack/echo/v4"
	"go.uber.org/zap"
)

func Configure(e *echo.Echo, l *zap.Logger) {
	e.GET("/api/v1/system/health", web.WrapPublic(Get, l))
}
