// Package connections hosts the /api/v1/connections CRUD: list, create
// with token-ownership + uniqueness validation, delete with
// tenant-scoped ownership check.
package connections

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/karnstack/tempo/internal/api/web"
	intauth "github.com/karnstack/tempo/internal/auth"
	"github.com/karnstack/tempo/internal/config"
	"github.com/karnstack/tempo/internal/storage/sqlite/sqlitedb"
	"github.com/labstack/echo/v4"
	"go.uber.org/zap"
)

// Connection kinds. The schema column is plain TEXT (no CHECK
// constraint per the project's "no DB-level constraints" rule), so
// validation lives entirely in this handler.
const (
	KindRepo = "repo"
	KindOrg  = "org"
)

// ConnectionDTO is the wire shape. tenant_id is never serialised.
type ConnectionDTO struct {
	ID           int64      `json:"id"`
	Kind         string     `json:"kind"`
	Owner        string     `json:"owner"`
	Name         *string    `json:"name"`
	TokenID      int64      `json:"token_id"`
	BackfillFrom time.Time  `json:"backfill_from"`
	Status       string     `json:"status"`
	LastSyncAt   *time.Time `json:"last_sync_at"`
	CreatedAt    time.Time  `json:"created_at"`
}

type ListConnectionsResponse struct {
	Connections []ConnectionDTO `json:"connections"`
}

type CreateConnectionRequest struct {
	Kind         string     `json:"kind"`
	Owner        string     `json:"owner"`
	Name         *string    `json:"name"`
	TokenID      int64      `json:"token_id"`
	BackfillFrom *time.Time `json:"backfill_from"`
}

type CreateConnectionResponse struct {
	Connection ConnectionDTO `json:"connection"`
}

// Configure mounts the three connection routes behind RequireSession.
func Configure(e *echo.Echo, _ *zap.Logger, m *intauth.Manager, q *sqlitedb.Queries, cfg *config.Config) {
	g := e.Group("/api/v1", web.RequireSession(m))
	g.GET("/connections", listHandler(q))
	g.POST("/connections", createHandler(q, cfg))
	g.DELETE("/connections/:id", deleteHandler(q))
}

func listHandler(q *sqlitedb.Queries) echo.HandlerFunc {
	return web.WrapPublic(func(ctx *web.Context) error {
		tenantID, err := tenantIDFromSession(ctx, q)
		if err != nil {
			return err
		}
		rows, err := q.ListConnectionsByTenant(ctx.Request().Context(), tenantID)
		if err != nil {
			ctx.L.Error("list connections failed", zap.Error(err))
			return echo.NewHTTPError(http.StatusInternalServerError, "list failed")
		}
		out := make([]ConnectionDTO, 0, len(rows))
		for _, r := range rows {
			out = append(out, dtoFrom(r))
		}
		return ctx.JSON(http.StatusOK, ListConnectionsResponse{Connections: out})
	})
}

func createHandler(q *sqlitedb.Queries, cfg *config.Config) echo.HandlerFunc {
	return web.WrapPublic(func(ctx *web.Context) error {
		var req CreateConnectionRequest
		if err := ctx.Bind(&req); err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "invalid request body")
		}

		req.Kind = strings.TrimSpace(req.Kind)
		req.Owner = strings.TrimSpace(req.Owner)
		var nameStr string
		if req.Name != nil {
			nameStr = strings.TrimSpace(*req.Name)
		}

		switch req.Kind {
		case KindRepo:
			if nameStr == "" {
				return echo.NewHTTPError(http.StatusBadRequest, "name is required for kind=repo")
			}
		case KindOrg:
			if nameStr != "" {
				return echo.NewHTTPError(http.StatusBadRequest, "name must be empty for kind=org")
			}
		default:
			return echo.NewHTTPError(http.StatusBadRequest, `kind must be "repo" or "org"`)
		}
		if req.Owner == "" {
			return echo.NewHTTPError(http.StatusBadRequest, "owner is required")
		}

		tenantID, err := tenantIDFromSession(ctx, q)
		if err != nil {
			return err
		}

		// Token must exist and belong to caller's tenant. Use 400 +
		// generic message for both not-found and cross-tenant so we
		// don't leak existence.
		token, err := q.GetGhToken(ctx.Request().Context(), req.TokenID)
		if errors.Is(err, sql.ErrNoRows) {
			return echo.NewHTTPError(http.StatusBadRequest, "token_id is invalid")
		}
		if err != nil {
			ctx.L.Error("get token failed", zap.Error(err))
			return echo.NewHTTPError(http.StatusInternalServerError, "lookup failed")
		}
		if token.TenantID != tenantID {
			return echo.NewHTTPError(http.StatusBadRequest, "token_id is invalid")
		}

		backfillFrom := time.Now().UTC().AddDate(0, 0, -cfg.Poll.BackfillDays)
		if req.BackfillFrom != nil {
			backfillFrom = req.BackfillFrom.UTC()
		}

		// Partial unique indices (connections_repo_uniq /
		// connections_org_uniq) enforce dedupe; map the constraint
		// violation to 409 via substring match because the
		// modernc.org/sqlite Error type's extended code isn't a
		// stable public field.
		var nameParam *string
		if req.Kind == KindRepo {
			nameParam = &nameStr
		}
		row, err := q.CreateConnection(ctx.Request().Context(), sqlitedb.CreateConnectionParams{
			TenantID:     tenantID,
			Kind:         req.Kind,
			Owner:        req.Owner,
			Name:         nameParam,
			TokenID:      req.TokenID,
			BackfillFrom: backfillFrom,
			Status:       "active",
		})
		if err != nil {
			if isUniqueViolation(err) {
				return echo.NewHTTPError(http.StatusConflict, "connection already exists")
			}
			ctx.L.Error("create connection failed", zap.Error(err))
			return echo.NewHTTPError(http.StatusInternalServerError, "create failed")
		}

		return ctx.JSON(http.StatusCreated, CreateConnectionResponse{Connection: dtoFrom(row)})
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

		row, err := q.GetConnection(ctx.Request().Context(), id)
		if errors.Is(err, sql.ErrNoRows) {
			return echo.NewHTTPError(http.StatusNotFound, "connection not found")
		}
		if err != nil {
			ctx.L.Error("get connection failed", zap.Error(err))
			return echo.NewHTTPError(http.StatusInternalServerError, "lookup failed")
		}
		if row.TenantID != tenantID {
			return echo.NewHTTPError(http.StatusNotFound, "connection not found")
		}

		if err := q.DeleteConnection(ctx.Request().Context(), id); err != nil {
			ctx.L.Error("delete connection failed", zap.Error(err))
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

func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "UNIQUE constraint failed")
}

func dtoFrom(r sqlitedb.Connection) ConnectionDTO {
	return ConnectionDTO{
		ID:           r.ID,
		Kind:         r.Kind,
		Owner:        r.Owner,
		Name:         r.Name,
		TokenID:      r.TokenID,
		BackfillFrom: r.BackfillFrom,
		Status:       r.Status,
		LastSyncAt:   r.LastSyncAt,
		CreatedAt:    r.CreatedAt,
	}
}
