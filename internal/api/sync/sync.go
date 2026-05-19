// Package sync hosts /api/v1/sync/status — per-connection ingest
// health snapshots for the caller's tenant. Powered by
// ingest.StatusFor (0031).
package sync

import (
	"database/sql"
	"errors"
	"net/http"
	"time"

	"github.com/karnstack/tempo/internal/api/web"
	intauth "github.com/karnstack/tempo/internal/auth"
	"github.com/karnstack/tempo/internal/ingest"
	"github.com/karnstack/tempo/internal/storage/sqlite/sqlitedb"
	"github.com/labstack/echo/v4"
	"go.uber.org/zap"
)

// SyncRunDTO mirrors sqlitedb.SyncRun on the wire. Fields that the
// dashboard doesn't read (connection_id duplicated at the parent
// level) are intentionally omitted.
type SyncRunDTO struct {
	ID                 int64      `json:"id"`
	StartedAt          time.Time  `json:"started_at"`
	FinishedAt         *time.Time `json:"finished_at"`
	Ok                 int64      `json:"ok"`
	Items              int64      `json:"items"`
	RateLimitRemaining *int64     `json:"rate_limit_remaining"`
	Error              string     `json:"error"`
}

type ConnectionSyncDTO struct {
	ConnectionID int64       `json:"connection_id"`
	Kind         string      `json:"kind"`
	Owner        string      `json:"owner"`
	Name         *string     `json:"name"`
	Status       string      `json:"status"`
	LastSyncAt   *time.Time  `json:"last_sync_at"`
	LatestRun    *SyncRunDTO `json:"latest_run"`
	LastSuccess  *SyncRunDTO `json:"last_success"`
	LastFailure  *SyncRunDTO `json:"last_failure"`
}

type SyncStatusResponse struct {
	Connections []ConnectionSyncDTO `json:"connections"`
}

// Configure mounts /api/v1/sync/status behind RequireSession.
func Configure(e *echo.Echo, _ *zap.Logger, m *intauth.Manager, q *sqlitedb.Queries) {
	g := e.Group("/api/v1", web.RequireSession(m))
	g.GET("/sync/status", statusHandler(q))
}

func statusHandler(q *sqlitedb.Queries) echo.HandlerFunc {
	return web.WrapPublic(func(ctx *web.Context) error {
		tenantID, err := tenantIDFromSession(ctx, q)
		if err != nil {
			return err
		}
		conns, err := q.ListConnectionsByTenant(ctx.Request().Context(), tenantID)
		if err != nil {
			ctx.L.Error("list connections failed", zap.Error(err))
			return echo.NewHTTPError(http.StatusInternalServerError, "list failed")
		}
		out := make([]ConnectionSyncDTO, 0, len(conns))
		for _, c := range conns {
			st, err := ingest.StatusFor(ctx.Request().Context(), q, c.ID)
			if err != nil {
				ctx.L.Error("status for connection failed",
					zap.Int64("connection_id", c.ID), zap.Error(err))
				return echo.NewHTTPError(http.StatusInternalServerError, "status failed")
			}
			out = append(out, ConnectionSyncDTO{
				ConnectionID: c.ID,
				Kind:         c.Kind,
				Owner:        c.Owner,
				Name:         c.Name,
				Status:       c.Status,
				LastSyncAt:   c.LastSyncAt,
				LatestRun:    runDTO(st.Latest),
				LastSuccess:  runDTO(st.LastSuccess),
				LastFailure:  runDTO(st.LastFailure),
			})
		}
		return ctx.JSON(http.StatusOK, SyncStatusResponse{Connections: out})
	})
}

func runDTO(r *sqlitedb.SyncRun) *SyncRunDTO {
	if r == nil {
		return nil
	}
	return &SyncRunDTO{
		ID:                 r.ID,
		StartedAt:          r.StartedAt,
		FinishedAt:         r.FinishedAt,
		Ok:                 r.Ok,
		Items:              r.Items,
		RateLimitRemaining: r.RateLimitRemaining,
		Error:              r.Error,
	}
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
