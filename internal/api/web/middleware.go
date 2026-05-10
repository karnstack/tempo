package web

import (
	"net/http"

	"github.com/karnstack/tempo/internal/auth"
	"github.com/labstack/echo/v4"
)

// RequireSession returns echo middleware that 401s any request without a
// valid session cookie. On success the validated session is attached to the
// request context so handlers can pull it via auth.FromContext.
func RequireSession(m *auth.Manager) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			sess, err := m.Validate(c.Request().Context(), c.Request())
			if err != nil {
				return echo.NewHTTPError(http.StatusUnauthorized, "unauthorized")
			}
			r := c.Request()
			c.SetRequest(r.WithContext(auth.IntoContext(r.Context(), sess)))
			return next(c)
		}
	}
}
