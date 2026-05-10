// Package tokens hosts the /api/v1/tokens CRUD: list, create with PAT
// encryption, delete with referential safety against connections.
package tokens

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/karnstack/tempo/internal/api/web"
	intauth "github.com/karnstack/tempo/internal/auth"
	"github.com/karnstack/tempo/internal/secret"
	"github.com/karnstack/tempo/internal/storage/sqlite/sqlitedb"
	"github.com/labstack/echo/v4"
	"go.uber.org/zap"
)

// TokenDTO is the wire shape — never carries the PAT.
type TokenDTO struct {
	ID        int64      `json:"id"`
	Label     string     `json:"label"`
	Scopes    string     `json:"scopes"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
}

type ListTokensResponse struct {
	Tokens []TokenDTO `json:"tokens"`
}

type CreateTokenRequest struct {
	Label     string     `json:"label"`
	PAT       string     `json:"pat"`
	Scopes    string     `json:"scopes"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

type CreateTokenResponse struct {
	Token TokenDTO `json:"token"`
}

type DeleteConflictResponse struct {
	Error           string `json:"error"`
	ConnectionCount int64  `json:"connection_count"`
}

// Configure mounts /api/v1/tokens behind RequireSession.
func Configure(e *echo.Echo, _ *zap.Logger, m *intauth.Manager, q *sqlitedb.Queries, box *secret.Box) {
	g := e.Group("/api/v1", web.RequireSession(m))
	g.GET("/tokens", listHandler(q))
	g.POST("/tokens", createHandler(q, box))
	g.DELETE("/tokens/:id", deleteHandler(q))
}

func listHandler(q *sqlitedb.Queries) echo.HandlerFunc {
	return web.WrapPublic(func(ctx *web.Context) error {
		tenantID, err := tenantIDFromSession(ctx, q)
		if err != nil {
			return err
		}
		rows, err := q.ListGhTokensByTenant(ctx.Request().Context(), tenantID)
		if err != nil {
			ctx.L.Error("list tokens failed", zap.Error(err))
			return echo.NewHTTPError(http.StatusInternalServerError, "list failed")
		}
		out := make([]TokenDTO, 0, len(rows))
		for _, r := range rows {
			out = append(out, dtoFrom(r))
		}
		return ctx.JSON(http.StatusOK, ListTokensResponse{Tokens: out})
	})
}

func createHandler(q *sqlitedb.Queries, box *secret.Box) echo.HandlerFunc {
	return web.WrapPublic(func(ctx *web.Context) error {
		var req CreateTokenRequest
		if err := ctx.Bind(&req); err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "invalid request body")
		}
		req.Label = strings.TrimSpace(req.Label)
		req.PAT = strings.TrimSpace(req.PAT)
		req.Scopes = strings.TrimSpace(req.Scopes)
		if req.Label == "" {
			return echo.NewHTTPError(http.StatusBadRequest, "label is required")
		}
		if req.PAT == "" {
			return echo.NewHTTPError(http.StatusBadRequest, "pat is required")
		}

		tenantID, err := tenantIDFromSession(ctx, q)
		if err != nil {
			return err
		}

		ct, err := box.Encrypt([]byte(req.PAT))
		if err != nil {
			ctx.L.Error("encrypt pat failed", zap.Error(err))
			return echo.NewHTTPError(http.StatusInternalServerError, "encrypt failed")
		}

		row, err := q.CreateGhToken(ctx.Request().Context(), sqlitedb.CreateGhTokenParams{
			TenantID:     tenantID,
			Label:        req.Label,
			EncryptedPat: ct,
			Scopes:       req.Scopes,
			ExpiresAt:    req.ExpiresAt,
		})
		if err != nil {
			ctx.L.Error("create token failed", zap.Error(err))
			return echo.NewHTTPError(http.StatusInternalServerError, "create failed")
		}

		return ctx.JSON(http.StatusCreated, CreateTokenResponse{Token: dtoFrom(row)})
	})
}

func deleteHandler(q *sqlitedb.Queries) echo.HandlerFunc {
	return web.WrapPublic(func(ctx *web.Context) error {
		idStr := ctx.Param("id")
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "id must be an integer")
		}

		tenantID, err := tenantIDFromSession(ctx, q)
		if err != nil {
			return err
		}

		row, err := q.GetGhToken(ctx.Request().Context(), id)
		if errors.Is(err, sql.ErrNoRows) {
			return echo.NewHTTPError(http.StatusNotFound, "token not found")
		}
		if err != nil {
			ctx.L.Error("get token failed", zap.Error(err))
			return echo.NewHTTPError(http.StatusInternalServerError, "lookup failed")
		}
		if row.TenantID != tenantID {
			return echo.NewHTTPError(http.StatusNotFound, "token not found")
		}

		count, err := q.CountConnectionsByToken(ctx.Request().Context(), id)
		if err != nil {
			ctx.L.Error("count connections by token failed", zap.Error(err))
			return echo.NewHTTPError(http.StatusInternalServerError, "lookup failed")
		}
		if count > 0 {
			return ctx.JSON(http.StatusConflict, DeleteConflictResponse{
				Error:           "token in use",
				ConnectionCount: count,
			})
		}

		if err := q.DeleteGhToken(ctx.Request().Context(), id); err != nil {
			ctx.L.Error("delete token failed", zap.Error(err))
			return echo.NewHTTPError(http.StatusInternalServerError, "delete failed")
		}
		return ctx.NoContent(http.StatusNoContent)
	})
}

func tenantIDFromSession(ctx *web.Context, q *sqlitedb.Queries) (int64, error) {
	sess, ok := intauth.FromContext(ctx.Request().Context())
	if !ok {
		return 0, echo.NewHTTPError(http.StatusUnauthorized, "unauthorized")
	}
	user, err := q.GetUser(ctx.Request().Context(), sess.UserID)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, echo.NewHTTPError(http.StatusUnauthorized, "unauthorized")
	}
	if err != nil {
		ctx.L.Error("get user failed", zap.Error(err))
		return 0, echo.NewHTTPError(http.StatusInternalServerError, "lookup failed")
	}
	return user.TenantID, nil
}

func dtoFrom(r sqlitedb.GhToken) TokenDTO {
	return TokenDTO{
		ID:        r.ID,
		Label:     r.Label,
		Scopes:    r.Scopes,
		ExpiresAt: r.ExpiresAt,
		CreatedAt: r.CreatedAt,
	}
}
