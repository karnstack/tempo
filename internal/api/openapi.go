package api

import (
	_ "embed"
	"net/http"

	"github.com/labstack/echo/v4"
)

//go:embed openapi.yaml
var openapiYAML []byte

// configureOpenAPI mounts GET /api/openapi.yaml. Public — no
// authentication — so external codegen tools (openapi-typescript,
// redocly) can pull the spec without a session cookie.
func configureOpenAPI(e *echo.Echo) {
	e.GET("/api/openapi.yaml", func(c echo.Context) error {
		return c.Blob(http.StatusOK, "application/yaml", openapiYAML)
	})
}
