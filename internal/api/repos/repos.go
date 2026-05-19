// Package repos hosts /api/v1/repos and /api/v1/repos/:owner/:name/metrics —
// the tenant-scoped repo selector and the per-repo metrics time-series
// the dashboard renders.
//
// All data is sourced from the daily rollup tables (daily_repo_stats,
// daily_review_latency, daily_review_load) populated by 0033-0036. Raw
// event tables are never scanned at request time.
package repos

import (
	"database/sql"
	"errors"
	"net/http"
	"time"

	"github.com/karnstack/tempo/internal/api/web"
	intauth "github.com/karnstack/tempo/internal/auth"
	"github.com/karnstack/tempo/internal/config"
	"github.com/karnstack/tempo/internal/storage/sqlite/sqlitedb"
	"github.com/labstack/echo/v4"
	"go.uber.org/zap"
)

// MaxRangeDays caps `?from`-`?to` so a single response can't drag in
// multi-year per-reviewer daily_review_load rows.
const MaxRangeDays = 365

// DefaultRangeDays is the rolling window when query params are absent.
const DefaultRangeDays = 30

// dateFormat matches what the rollup tables store and what callers send
// via query params.
const dateFormat = "2006-01-02"

// RepoDTO is the wire shape. tenant_id / connection_id / gh_id /
// added_at are intentionally not serialised.
type RepoDTO struct {
	ID            int64  `json:"id"`
	Owner         string `json:"owner"`
	Name          string `json:"name"`
	DefaultBranch string `json:"default_branch"`
	Archived      bool   `json:"archived"`
}

type ListReposResponse struct {
	Repos []RepoDTO `json:"repos"`
}

type DailyRepoStatDTO struct {
	Date               string `json:"date"`
	PrsOpened          int64  `json:"prs_opened"`
	PrsMerged          int64  `json:"prs_merged"`
	PrsClosed          int64  `json:"prs_closed"`
	Deploys            int64  `json:"deploys"`
	LeadTimeSecondsP50 *int64 `json:"lead_time_seconds_p50"`
	LeadTimeSecondsP90 *int64 `json:"lead_time_seconds_p90"`
}

type DailyReviewLatencyDTO struct {
	Date                        string `json:"date"`
	TimeToFirstReviewSecondsP50 *int64 `json:"time_to_first_review_seconds_p50"`
	TimeToFirstReviewSecondsP90 *int64 `json:"time_to_first_review_seconds_p90"`
	Count                       int64  `json:"count"`
}

type DailyReviewLoadDTO struct {
	Date               string `json:"date"`
	ReviewerGhUserID   int64  `json:"reviewer_gh_user_id"`
	Reviews            int64  `json:"reviews"`
	ResponseMinutesP50 *int64 `json:"response_minutes_p50"`
}

type MetricsResponse struct {
	Repo               RepoDTO                 `json:"repo"`
	From               string                  `json:"from"`
	To                 string                  `json:"to"`
	DailyStats         []DailyRepoStatDTO      `json:"daily_stats"`
	DailyReviewLatency []DailyReviewLatencyDTO `json:"daily_review_latency"`
	DailyReviewLoad    []DailyReviewLoadDTO    `json:"daily_review_load"`
}

// Configure mounts the repo endpoints behind RequireSession.
func Configure(e *echo.Echo, _ *zap.Logger, m *intauth.Manager, q *sqlitedb.Queries, cfg *config.Config) {
	g := e.Group("/api/v1", web.RequireSession(m))
	g.GET("/repos", listHandler(q))
	g.GET("/repos/:owner/:name/metrics", metricsHandler(q, cfg))
}

func listHandler(q *sqlitedb.Queries) echo.HandlerFunc {
	return web.WrapPublic(func(ctx *web.Context) error {
		tenantID, err := tenantIDFromSession(ctx, q)
		if err != nil {
			return err
		}
		rows, err := q.ListReposByTenant(ctx.Request().Context(), tenantID)
		if err != nil {
			ctx.L.Error("list repos failed", zap.Error(err))
			return echo.NewHTTPError(http.StatusInternalServerError, "list failed")
		}
		out := make([]RepoDTO, 0, len(rows))
		for _, r := range rows {
			out = append(out, dtoFrom(r))
		}
		return ctx.JSON(http.StatusOK, ListReposResponse{Repos: out})
	})
}

func metricsHandler(q *sqlitedb.Queries, cfg *config.Config) echo.HandlerFunc {
	return web.WrapPublic(func(ctx *web.Context) error {
		owner := ctx.Param("owner")
		name := ctx.Param("name")
		if owner == "" || name == "" {
			return echo.NewHTTPError(http.StatusBadRequest, "owner and name are required")
		}

		tz := tzFromCfg(cfg)
		fromStr := ctx.QueryParam("from")
		toStr := ctx.QueryParam("to")

		from, to, err := parseDateRange(fromStr, toStr, tz)
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, err.Error())
		}

		tenantID, err := tenantIDFromSession(ctx, q)
		if err != nil {
			return err
		}

		repo, err := q.GetRepoByTenantOwnerName(ctx.Request().Context(), sqlitedb.GetRepoByTenantOwnerNameParams{
			TenantID: tenantID,
			Owner:    owner,
			Name:     name,
		})
		if errors.Is(err, sql.ErrNoRows) {
			return echo.NewHTTPError(http.StatusNotFound, "repo not found")
		}
		if err != nil {
			ctx.L.Error("get repo failed", zap.Error(err))
			return echo.NewHTTPError(http.StatusInternalServerError, "lookup failed")
		}

		// Existing list queries are half-open [from, to); the API is
		// inclusive on both ends, so bump to_exclusive by one day.
		fromDate := from.Format(dateFormat)
		toExclusive := to.AddDate(0, 0, 1).Format(dateFormat)

		statsRows, err := q.ListDailyRepoStatsByRepoBetween(ctx.Request().Context(), sqlitedb.ListDailyRepoStatsByRepoBetweenParams{
			RepoID:   repo.ID,
			FromDate: fromDate,
			ToDate:   toExclusive,
		})
		if err != nil {
			ctx.L.Error("list daily_repo_stats failed", zap.Error(err))
			return echo.NewHTTPError(http.StatusInternalServerError, "lookup failed")
		}
		latencyRows, err := q.ListDailyReviewLatencyByRepoBetween(ctx.Request().Context(), sqlitedb.ListDailyReviewLatencyByRepoBetweenParams{
			RepoID:   repo.ID,
			FromDate: fromDate,
			ToDate:   toExclusive,
		})
		if err != nil {
			ctx.L.Error("list daily_review_latency failed", zap.Error(err))
			return echo.NewHTTPError(http.StatusInternalServerError, "lookup failed")
		}
		loadRows, err := q.ListDailyReviewLoadByRepoBetween(ctx.Request().Context(), sqlitedb.ListDailyReviewLoadByRepoBetweenParams{
			RepoID:   repo.ID,
			FromDate: fromDate,
			ToDate:   toExclusive,
		})
		if err != nil {
			ctx.L.Error("list daily_review_load failed", zap.Error(err))
			return echo.NewHTTPError(http.StatusInternalServerError, "lookup failed")
		}

		stats := make([]DailyRepoStatDTO, 0, len(statsRows))
		for _, r := range statsRows {
			stats = append(stats, DailyRepoStatDTO{
				Date:               r.Date,
				PrsOpened:          r.PrsOpened,
				PrsMerged:          r.PrsMerged,
				PrsClosed:          r.PrsClosed,
				Deploys:            r.Deploys,
				LeadTimeSecondsP50: r.LeadTimeSecondsP50,
				LeadTimeSecondsP90: r.LeadTimeSecondsP90,
			})
		}
		latency := make([]DailyReviewLatencyDTO, 0, len(latencyRows))
		for _, r := range latencyRows {
			latency = append(latency, DailyReviewLatencyDTO{
				Date:                        r.Date,
				TimeToFirstReviewSecondsP50: r.TimeToFirstReviewSecondsP50,
				TimeToFirstReviewSecondsP90: r.TimeToFirstReviewSecondsP90,
				Count:                       r.Count,
			})
		}
		load := make([]DailyReviewLoadDTO, 0, len(loadRows))
		for _, r := range loadRows {
			load = append(load, DailyReviewLoadDTO{
				Date:               r.Date,
				ReviewerGhUserID:   r.ReviewerGhUserID,
				Reviews:            r.Reviews,
				ResponseMinutesP50: r.ResponseMinutesP50,
			})
		}

		return ctx.JSON(http.StatusOK, MetricsResponse{
			Repo:               dtoFrom(repo),
			From:               fromDate,
			To:                 to.Format(dateFormat),
			DailyStats:         stats,
			DailyReviewLatency: latency,
			DailyReviewLoad:    load,
		})
	})
}

// parseDateRange validates `from` / `to` query strings and returns
// inclusive local-midnight times. Missing params default to a rolling
// DefaultRangeDays window ending today.
func parseDateRange(fromStr, toStr string, tz *time.Location) (time.Time, time.Time, error) {
	now := time.Now().In(tz)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, tz)

	var to time.Time
	if toStr == "" {
		to = today
	} else {
		t, err := time.ParseInLocation(dateFormat, toStr, tz)
		if err != nil {
			return time.Time{}, time.Time{}, errors.New(`"to" must be YYYY-MM-DD`)
		}
		to = t
	}

	var from time.Time
	if fromStr == "" {
		from = to.AddDate(0, 0, -(DefaultRangeDays - 1))
	} else {
		t, err := time.ParseInLocation(dateFormat, fromStr, tz)
		if err != nil {
			return time.Time{}, time.Time{}, errors.New(`"from" must be YYYY-MM-DD`)
		}
		from = t
	}

	if to.Before(from) {
		return time.Time{}, time.Time{}, errors.New(`"to" must not be before "from"`)
	}
	// Whole-day distance check (inclusive). 365 days inclusive means
	// to - from <= 364.
	maxDays := MaxRangeDays - 1
	if to.Sub(from) > time.Duration(maxDays)*24*time.Hour {
		return time.Time{}, time.Time{}, errors.New("date range exceeds 365 days")
	}
	return from, to, nil
}

func tzFromCfg(cfg *config.Config) *time.Location {
	if cfg != nil && cfg.Rollup.Timezone != nil {
		return cfg.Rollup.Timezone
	}
	return time.Local
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

func dtoFrom(r sqlitedb.Repo) RepoDTO {
	return RepoDTO{
		ID:            r.ID,
		Owner:         r.Owner,
		Name:          r.Name,
		DefaultBranch: r.DefaultBranch,
		Archived:      r.Archived != 0,
	}
}
