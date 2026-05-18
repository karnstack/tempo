// Package engineerstats implements the rollup.Aggregator that rebuilds
// daily_engineer_stats from commits, PRs, reviews, and comments.
//
// The rollup is keyed by (date, repo_id, gh_user_id). For a given local
// date `D` in the scheduler's tz, the aggregator deletes the existing
// (date, repo_id) slice and re-aggregates from the raw event tables.
// Each repo runs in its own short transaction so a failure leaves the
// rest of the day's slice untouched and the next run can retry just
// that repo.
package engineerstats

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/karnstack/tempo/internal/storage"
	"github.com/karnstack/tempo/internal/storage/sqlite/sqlitedb"
	"go.uber.org/zap"
)

// aggregatorName is the value returned by Name() and shown in logs.
// Per-aggregator kinds aren't yet recorded in rollup_runs (0037 may add
// them when manual reruns land); this is purely a human-readable tag.
const aggregatorName = "engineer_stats"

// aggregateSQL rebuilds the (date, repo_id) slice of daily_engineer_stats
// in a single statement. Parameters (positional, ?1 .. ?4):
//
//	?1 = repo_id     (int64)
//	?2 = from_ts     (UTC time)
//	?3 = to_ts       (UTC time, exclusive)
//	?4 = date_str    (YYYY-MM-DD in scheduler tz)
//
// Each ?i may appear multiple times — the driver substitutes them by
// position-of-occurrence in the SQL text. Numbered placeholders make
// that one-to-many mapping explicit.
//
// gh_user_id=0 (the Ghost sentinel from commits ingest) is filtered out
// in every CTE; archived-repo filtering happens in Go.
const aggregateSQL = `
WITH
  commits_agg AS (
    SELECT author_gh_user_id AS uid, COUNT(*) AS n
    FROM commits
    WHERE repo_id = ?1
      AND author_gh_user_id != 0
      AND authored_at >= ?2 AND authored_at < ?3
    GROUP BY author_gh_user_id
  ),
  prs_opened_agg AS (
    SELECT author_gh_user_id AS uid, COUNT(*) AS n
    FROM pull_requests
    WHERE repo_id = ?1
      AND author_gh_user_id != 0
      AND created_at >= ?2 AND created_at < ?3
    GROUP BY author_gh_user_id
  ),
  prs_merged_agg AS (
    SELECT author_gh_user_id AS uid,
           COUNT(*)        AS n,
           SUM(additions)  AS adds,
           SUM(deletions)  AS dels
    FROM pull_requests
    WHERE repo_id = ?1
      AND merged_at IS NOT NULL
      AND author_gh_user_id != 0
      AND merged_at >= ?2 AND merged_at < ?3
    GROUP BY author_gh_user_id
  ),
  reviews_agg AS (
    SELECT reviewer_gh_user_id AS uid, COUNT(*) AS n
    FROM pr_reviews
    WHERE pr_repo_id = ?1
      AND reviewer_gh_user_id != 0
      AND submitted_at >= ?2 AND submitted_at < ?3
    GROUP BY reviewer_gh_user_id
  ),
  comments_agg AS (
    SELECT uid, SUM(n) AS n FROM (
      SELECT author_gh_user_id AS uid, COUNT(*) AS n
      FROM pr_review_comments
      WHERE pr_repo_id = ?1
        AND author_gh_user_id != 0
        AND created_at >= ?2 AND created_at < ?3
      GROUP BY author_gh_user_id
      UNION ALL
      SELECT author_gh_user_id AS uid, COUNT(*) AS n
      FROM pr_issue_comments
      WHERE pr_repo_id = ?1
        AND author_gh_user_id != 0
        AND created_at >= ?2 AND created_at < ?3
      GROUP BY author_gh_user_id
    ) GROUP BY uid
  ),
  users AS (
    SELECT uid FROM commits_agg
    UNION SELECT uid FROM prs_opened_agg
    UNION SELECT uid FROM prs_merged_agg
    UNION SELECT uid FROM reviews_agg
    UNION SELECT uid FROM comments_agg
  )
INSERT INTO daily_engineer_stats (
  date, repo_id, gh_user_id,
  commits, prs_opened, prs_merged, reviews_given, comments,
  additions, deletions
)
SELECT
  ?4, ?1, u.uid,
  COALESCE(c.n, 0),
  COALESCE(po.n, 0),
  COALESCE(pm.n, 0),
  COALESCE(rv.n, 0),
  COALESCE(cm.n, 0),
  COALESCE(pm.adds, 0),
  COALESCE(pm.dels, 0)
FROM users u
LEFT JOIN commits_agg     c  ON c.uid  = u.uid
LEFT JOIN prs_opened_agg  po ON po.uid = u.uid
LEFT JOIN prs_merged_agg  pm ON pm.uid = u.uid
LEFT JOIN reviews_agg     rv ON rv.uid = u.uid
LEFT JOIN comments_agg    cm ON cm.uid = u.uid
ON CONFLICT (date, repo_id, gh_user_id) DO UPDATE SET
  commits       = excluded.commits,
  prs_opened    = excluded.prs_opened,
  prs_merged    = excluded.prs_merged,
  reviews_given = excluded.reviews_given,
  comments      = excluded.comments,
  additions     = excluded.additions,
  deletions     = excluded.deletions;
`

// Aggregator is the rollup.Aggregator implementation for engineer
// stats. It holds a direct *sql.DB so it can manage per-repo
// transactions itself — sqlc's WithTx covers the typed DELETE but not
// the raw INSERT...SELECT.
type Aggregator struct {
	db  *sql.DB
	q   *sqlitedb.Queries
	log *zap.Logger
}

// New builds an Aggregator from the shared Storage. The fx wiring is
// in module.go.
func New(s storage.Storage, l *zap.Logger) *Aggregator {
	db := s.DB()
	return &Aggregator{db: db, q: sqlitedb.New(db), log: l}
}

// Name implements rollup.Aggregator.
func (*Aggregator) Name() string { return aggregatorName }

// Aggregate rebuilds daily_engineer_stats for `date` across every
// non-archived repo. Per-repo errors are logged and the first one is
// returned, but they never short-circuit sibling repos — matches the
// scheduler's policy for aggregator-level errors.
//
// `date` is local-midnight in the scheduler's tz. We treat it as the
// inclusive start of a 24h window and compare against the UTC
// TIMESTAMP columns directly — Go's time.Time carries the location and
// the driver normalises at the SQL boundary.
func (a *Aggregator) Aggregate(ctx context.Context, date time.Time) error {
	dateStr := date.Format("2006-01-02")
	fromTS := date
	toTS := date.AddDate(0, 0, 1)

	repos, err := a.q.ListAllRepos(ctx)
	if err != nil {
		return fmt.Errorf("list repos: %w", err)
	}

	var firstErr error
	for _, r := range repos {
		if r.Archived != 0 {
			continue
		}
		if err := a.aggregateRepo(ctx, r.ID, dateStr, fromTS, toTS); err != nil {
			a.log.Warn("rollup/engineer_stats: repo failed",
				zap.Int64("repo_id", r.ID),
				zap.String("date", dateStr),
				zap.Error(err))
			if firstErr == nil {
				firstErr = fmt.Errorf("repo %d: %w", r.ID, err)
			}
		}
	}
	return firstErr
}

// aggregateRepo runs DELETE+INSERT inside a single tx so a partial
// failure leaves the (date, repo_id) slice untouched.
func (a *Aggregator) aggregateRepo(ctx context.Context, repoID int64, dateStr string, from, to time.Time) error {
	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	qtx := a.q.WithTx(tx)
	if err := qtx.DeleteDailyEngineerStatsByDateRepo(ctx, sqlitedb.DeleteDailyEngineerStatsByDateRepoParams{
		Date:   dateStr,
		RepoID: repoID,
	}); err != nil {
		return fmt.Errorf("delete existing: %w", err)
	}
	if _, err := tx.ExecContext(ctx, aggregateSQL, repoID, from, to, dateStr); err != nil {
		return fmt.Errorf("aggregate: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}
