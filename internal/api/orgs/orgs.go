// Package orgs hosts /api/v1/orgs/:org/metrics — the org-level
// summary that aggregates daily rollups across every repo with a
// given owner in the caller's tenant.
//
// Percentile columns (lead_time_seconds_p50/p90, etc.) are NOT
// aggregated; pooling them across repos without raw samples is
// statistically meaningless. The frontend reads per-repo
// percentiles from the repo endpoint.
package orgs

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

const (
	dateFormat       = "2006-01-02"
	defaultRangeDays = 30
	maxRangeDays     = 365
)

// RepoDTO is a trimmed projection of sqlitedb.Repo for the org
// response. Same shape as internal/api/repos.RepoDTO; defined
// locally to avoid a cross-package type dependency.
type RepoDTO struct {
	ID            int64  `json:"id"`
	Owner         string `json:"owner"`
	Name          string `json:"name"`
	DefaultBranch string `json:"default_branch"`
	Archived      bool   `json:"archived"`
}

type DailyStatsDTO struct {
	Date      string `json:"date"`
	PrsOpened int64  `json:"prs_opened"`
	PrsMerged int64  `json:"prs_merged"`
	PrsClosed int64  `json:"prs_closed"`
	Deploys   int64  `json:"deploys"`
}

type DailyReviewLatencyDTO struct {
	Date  string `json:"date"`
	Count int64  `json:"count"`
}

type DailyReviewLoadDTO struct {
	Date             string `json:"date"`
	ReviewerGhUserID int64  `json:"reviewer_gh_user_id"`
	Reviews          int64  `json:"reviews"`
}

type MetricsResponse struct {
	Org                string                  `json:"org"`
	From               string                  `json:"from"`
	To                 string                  `json:"to"`
	Repos              []RepoDTO               `json:"repos"`
	DailyStats         []DailyStatsDTO         `json:"daily_stats"`
	DailyReviewLatency []DailyReviewLatencyDTO `json:"daily_review_latency"`
	DailyReviewLoad    []DailyReviewLoadDTO    `json:"daily_review_load"`
}

// Configure mounts /api/v1/orgs/:org/metrics behind RequireSession.
func Configure(e *echo.Echo, _ *zap.Logger, m *intauth.Manager, q *sqlitedb.Queries, cfg *config.Config) {
	g := e.Group("/api/v1", web.RequireSession(m))
	g.GET("/orgs/:org/metrics", metricsHandler(q, cfg))
}

func metricsHandler(q *sqlitedb.Queries, cfg *config.Config) echo.HandlerFunc {
	return web.WrapPublic(func(ctx *web.Context) error {
		org := ctx.Param("org")
		if org == "" {
			return echo.NewHTTPError(http.StatusBadRequest, "org is required")
		}

		tz := tzFromCfg(cfg)
		from, to, err := parseDateRange(ctx.QueryParam("from"), ctx.QueryParam("to"), tz)
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, err.Error())
		}

		tenantID, err := tenantIDFromSession(ctx, q)
		if err != nil {
			return err
		}

		allRepos, err := q.ListReposByTenant(ctx.Request().Context(), tenantID)
		if err != nil {
			ctx.L.Error("list repos failed", zap.Error(err))
			return echo.NewHTTPError(http.StatusInternalServerError, "lookup failed")
		}
		orgRepos := make([]RepoDTO, 0, len(allRepos))
		for _, r := range allRepos {
			if r.Owner != org {
				continue
			}
			orgRepos = append(orgRepos, RepoDTO{
				ID:            r.ID,
				Owner:         r.Owner,
				Name:          r.Name,
				DefaultBranch: r.DefaultBranch,
				Archived:      r.Archived != 0,
			})
		}
		if len(orgRepos) == 0 {
			return echo.NewHTTPError(http.StatusNotFound, "org not found")
		}

		fromDate := from.Format(dateFormat)
		toExclusive := to.AddDate(0, 0, 1).Format(dateFormat)

		statsRows, err := q.SumDailyRepoStatsByTenantOwnerBetween(ctx.Request().Context(), sqlitedb.SumDailyRepoStatsByTenantOwnerBetweenParams{
			TenantID: tenantID,
			Owner:    org,
			FromDate: fromDate,
			ToDate:   toExclusive,
		})
		if err != nil {
			ctx.L.Error("sum daily_repo_stats failed", zap.Error(err))
			return echo.NewHTTPError(http.StatusInternalServerError, "lookup failed")
		}
		latencyRows, err := q.SumDailyReviewLatencyByTenantOwnerBetween(ctx.Request().Context(), sqlitedb.SumDailyReviewLatencyByTenantOwnerBetweenParams{
			TenantID: tenantID,
			Owner:    org,
			FromDate: fromDate,
			ToDate:   toExclusive,
		})
		if err != nil {
			ctx.L.Error("sum daily_review_latency failed", zap.Error(err))
			return echo.NewHTTPError(http.StatusInternalServerError, "lookup failed")
		}
		loadRows, err := q.SumDailyReviewLoadByTenantOwnerBetween(ctx.Request().Context(), sqlitedb.SumDailyReviewLoadByTenantOwnerBetweenParams{
			TenantID: tenantID,
			Owner:    org,
			FromDate: fromDate,
			ToDate:   toExclusive,
		})
		if err != nil {
			ctx.L.Error("sum daily_review_load failed", zap.Error(err))
			return echo.NewHTTPError(http.StatusInternalServerError, "lookup failed")
		}

		stats := make([]DailyStatsDTO, 0, len(statsRows))
		for _, r := range statsRows {
			stats = append(stats, DailyStatsDTO{
				Date:      r.Date,
				PrsOpened: r.PrsOpened,
				PrsMerged: r.PrsMerged,
				PrsClosed: r.PrsClosed,
				Deploys:   r.Deploys,
			})
		}
		latency := make([]DailyReviewLatencyDTO, 0, len(latencyRows))
		for _, r := range latencyRows {
			latency = append(latency, DailyReviewLatencyDTO{
				Date:  r.Date,
				Count: r.Count,
			})
		}
		load := make([]DailyReviewLoadDTO, 0, len(loadRows))
		for _, r := range loadRows {
			load = append(load, DailyReviewLoadDTO{
				Date:             r.Date,
				ReviewerGhUserID: r.ReviewerGhUserID,
				Reviews:          r.Reviews,
			})
		}

		return ctx.JSON(http.StatusOK, MetricsResponse{
			Org:                org,
			From:               fromDate,
			To:                 to.Format(dateFormat),
			Repos:              orgRepos,
			DailyStats:         stats,
			DailyReviewLatency: latency,
			DailyReviewLoad:    load,
		})
	})
}

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
		from = to.AddDate(0, 0, -(defaultRangeDays - 1))
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
	maxDays := maxRangeDays - 1
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
