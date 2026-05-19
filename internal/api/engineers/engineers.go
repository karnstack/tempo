// Package engineers hosts /api/v1/engineers and
// /api/v1/engineers/:login/metrics. The list endpoint powers the
// engineer-search drop-down; the metrics endpoint surfaces per-engineer
// daily authored stats (from daily_engineer_stats) and review-load
// fan-out (from daily_review_load).
package engineers

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

// EngineerDTO is the wire shape. tenant_id, last_seen_at, etc. are
// intentionally not surfaced.
type EngineerDTO struct {
	ID        int64   `json:"id"`
	GhID      int64   `json:"gh_id"`
	Login     string  `json:"login"`
	Name      *string `json:"name"`
	AvatarURL *string `json:"avatar_url"`
}

type ListEngineersResponse struct {
	Engineers []EngineerDTO `json:"engineers"`
}

type DailyEngineerStatDTO struct {
	Date         string `json:"date"`
	RepoID       int64  `json:"repo_id"`
	Commits      int64  `json:"commits"`
	PrsOpened    int64  `json:"prs_opened"`
	PrsMerged    int64  `json:"prs_merged"`
	ReviewsGiven int64  `json:"reviews_given"`
	Comments     int64  `json:"comments"`
	Additions    int64  `json:"additions"`
	Deletions    int64  `json:"deletions"`
}

type DailyReviewLoadDTO struct {
	Date               string `json:"date"`
	RepoID             int64  `json:"repo_id"`
	Reviews            int64  `json:"reviews"`
	ResponseMinutesP50 *int64 `json:"response_minutes_p50"`
}

type MetricsResponse struct {
	Engineer        EngineerDTO            `json:"engineer"`
	From            string                 `json:"from"`
	To              string                 `json:"to"`
	DailyStats      []DailyEngineerStatDTO `json:"daily_stats"`
	DailyReviewLoad []DailyReviewLoadDTO   `json:"daily_review_load"`
}

// Configure mounts the engineer endpoints behind RequireSession.
func Configure(e *echo.Echo, _ *zap.Logger, m *intauth.Manager, q *sqlitedb.Queries, cfg *config.Config) {
	g := e.Group("/api/v1", web.RequireSession(m))
	g.GET("/engineers", listHandler(q))
	g.GET("/engineers/:login/metrics", metricsHandler(q, cfg))
}

func listHandler(q *sqlitedb.Queries) echo.HandlerFunc {
	return web.WrapPublic(func(ctx *web.Context) error {
		tenantID, err := tenantIDFromSession(ctx, q)
		if err != nil {
			return err
		}
		rows, err := q.ListGhUsersByTenant(ctx.Request().Context(), tenantID)
		if err != nil {
			ctx.L.Error("list gh_users failed", zap.Error(err))
			return echo.NewHTTPError(http.StatusInternalServerError, "list failed")
		}
		out := make([]EngineerDTO, 0, len(rows))
		for _, r := range rows {
			out = append(out, dtoFrom(r))
		}
		return ctx.JSON(http.StatusOK, ListEngineersResponse{Engineers: out})
	})
}

func metricsHandler(q *sqlitedb.Queries, cfg *config.Config) echo.HandlerFunc {
	return web.WrapPublic(func(ctx *web.Context) error {
		login := ctx.Param("login")
		if login == "" {
			return echo.NewHTTPError(http.StatusBadRequest, "login is required")
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

		ghUser, err := q.GetGhUserByTenantLogin(ctx.Request().Context(), sqlitedb.GetGhUserByTenantLoginParams{
			TenantID: tenantID,
			Login:    login,
		})
		if errors.Is(err, sql.ErrNoRows) {
			return echo.NewHTTPError(http.StatusNotFound, "engineer not found")
		}
		if err != nil {
			ctx.L.Error("get gh_user failed", zap.Error(err))
			return echo.NewHTTPError(http.StatusInternalServerError, "lookup failed")
		}

		fromDate := from.Format(dateFormat)
		toExclusive := to.AddDate(0, 0, 1).Format(dateFormat)

		statsRows, err := q.ListDailyEngineerStatsByUserBetween(ctx.Request().Context(), sqlitedb.ListDailyEngineerStatsByUserBetweenParams{
			GhUserID: ghUser.ID,
			FromDate: fromDate,
			ToDate:   toExclusive,
		})
		if err != nil {
			ctx.L.Error("list daily_engineer_stats failed", zap.Error(err))
			return echo.NewHTTPError(http.StatusInternalServerError, "lookup failed")
		}
		loadRows, err := q.ListDailyReviewLoadByReviewerBetween(ctx.Request().Context(), sqlitedb.ListDailyReviewLoadByReviewerBetweenParams{
			ReviewerGhUserID: ghUser.ID,
			FromDate:         fromDate,
			ToDate:           toExclusive,
		})
		if err != nil {
			ctx.L.Error("list daily_review_load failed", zap.Error(err))
			return echo.NewHTTPError(http.StatusInternalServerError, "lookup failed")
		}

		stats := make([]DailyEngineerStatDTO, 0, len(statsRows))
		for _, r := range statsRows {
			stats = append(stats, DailyEngineerStatDTO{
				Date:         r.Date,
				RepoID:       r.RepoID,
				Commits:      r.Commits,
				PrsOpened:    r.PrsOpened,
				PrsMerged:    r.PrsMerged,
				ReviewsGiven: r.ReviewsGiven,
				Comments:     r.Comments,
				Additions:    r.Additions,
				Deletions:    r.Deletions,
			})
		}
		load := make([]DailyReviewLoadDTO, 0, len(loadRows))
		for _, r := range loadRows {
			load = append(load, DailyReviewLoadDTO{
				Date:               r.Date,
				RepoID:             r.RepoID,
				Reviews:            r.Reviews,
				ResponseMinutesP50: r.ResponseMinutesP50,
			})
		}

		return ctx.JSON(http.StatusOK, MetricsResponse{
			Engineer:        dtoFrom(ghUser),
			From:            fromDate,
			To:              to.Format(dateFormat),
			DailyStats:      stats,
			DailyReviewLoad: load,
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

func dtoFrom(u sqlitedb.GhUser) EngineerDTO {
	return EngineerDTO{
		ID:        u.ID,
		GhID:      u.GhID,
		Login:     u.Login,
		Name:      u.Name,
		AvatarURL: u.AvatarUrl,
	}
}
