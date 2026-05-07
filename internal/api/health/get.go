package health

import (
	"net/http"

	"github.com/karnstack/tempo/internal/api/web"
	"github.com/karnstack/tempo/internal/version"
)

type Response struct {
	Status  string `json:"status"`
	Version string `json:"version"`
}

func Get(ctx *web.Context) error {
	return ctx.JSON(http.StatusOK, Response{
		Status:  "ok",
		Version: version.Version,
	})
}
