package deployments

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/karnstack/tempo/internal/github"
	ghdeployments "github.com/karnstack/tempo/internal/github/deployments"
	"github.com/karnstack/tempo/internal/ingest"
	"github.com/karnstack/tempo/internal/storage/sqlite/sqlitedb"
	"go.uber.org/zap"
)

// pageSize is the per-request page size on `GET .../deployments`. 100 is
// GitHub's max and the deployments fetcher's clamp.
const pageSize = 100

// Runner ingests GitHub Deployments for one connection per call to Run.
type Runner struct {
	q   *sqlitedb.Queries
	log *zap.Logger
	now func() time.Time
}

// Option mutates a Runner after New. Used by tests to inject fakes.
type Option func(*Runner)

// WithClock overrides time.Now. Useful for deterministic cursor.updated_at
// timestamps in tests.
func WithClock(now func() time.Time) Option { return func(r *Runner) { r.now = now } }

// New builds a Runner with production defaults.
func New(q *sqlitedb.Queries, l *zap.Logger, opts ...Option) *Runner {
	r := &Runner{q: q, log: l, now: time.Now}
	for _, o := range opts {
		o(r)
	}
	return r
}

// Name implements ingest.Runner.
func (*Runner) Name() string { return "deployments" }

// Run implements ingest.Runner. The scheduler hands us a per-connection
// *github.Client; we own the per-repo iteration.
func (r *Runner) Run(ctx context.Context, conn sqlitedb.Connection, gh *github.Client) (ingest.Outcome, error) {
	repos, err := r.q.ListReposByConnection(ctx, conn.ID)
	if err != nil {
		return ingest.Outcome{}, fmt.Errorf("list repos: %w", err)
	}

	f := ghdeployments.New(gh)
	var (
		items    int64
		firstErr error
	)
	for _, repo := range repos {
		if repo.Archived != 0 {
			continue
		}
		n, perr := r.syncRepo(ctx, f, conn, repo)
		items += n
		if perr != nil {
			r.log.Warn("ingest/deployments: repo failed",
				zap.Int64("connection_id", conn.ID),
				zap.String("repo", repo.Owner+"/"+repo.Name),
				zap.Error(perr),
			)
			if firstErr == nil {
				firstErr = fmt.Errorf("%s/%s: %w", repo.Owner, repo.Name, perr)
			}
		}
	}

	out := ingest.Outcome{Items: items}
	if rem, ok := gh.RESTRemaining(); ok {
		v := int64(rem)
		out.RateLimitRemaining = &v
	}
	return out, firstErr
}

// syncRepo pages through deployments for one repo, upserts deploy rows
// whose created_at > cursor.since, and writes a new cursor at the end.
// Deploys come back DESC by created_at, so we early-stop the moment a
// page contains a deploy with created_at <= since.
//
// Cursor advance rules (see package doc):
//
//   - 304 NotModified → no cursor write.
//   - 200 OK, N>0 new deploys → (max(createdAt), page1.ETag).
//   - 200 OK, 0 new deploys (empty or all old) → (since unchanged, page1.ETag).
func (r *Runner) syncRepo(ctx context.Context, f *ghdeployments.Fetcher, conn sqlitedb.Connection, repo sqlitedb.Repo) (int64, error) {
	resource := "deployments:" + repo.Owner + "/" + repo.Name

	since, etag, err := r.loadCursor(ctx, conn.ID, resource, conn.BackfillFrom)
	if err != nil {
		return 0, err
	}

	// Page 1 — the only page that carries a meaningful etag.
	page1, err := f.Fetch(ctx, repo.Owner, repo.Name, ghdeployments.FetchOptions{
		ETag:    etag,
		PerPage: pageSize,
		Page:    1,
	})
	if err != nil {
		return 0, err
	}
	if page1.NotModified {
		return 0, nil
	}

	var (
		items       int64
		maxCreated  time.Time
		page        = page1
		stopPaging  bool
	)
	for {
		n, m, sawOld, perr := r.processPage(ctx, repo.ID, page, since)
		items += n
		if m.After(maxCreated) {
			maxCreated = m
		}
		if perr != nil {
			return items, perr
		}
		if sawOld {
			stopPaging = true
		}
		if stopPaging || !page.HasNext {
			break
		}
		next, perr := f.Fetch(ctx, repo.Owner, repo.Name, ghdeployments.FetchOptions{
			PerPage: pageSize,
			Page:    page.NextPage,
		})
		if perr != nil {
			return items, perr
		}
		page = next
	}

	newSince := since
	if !maxCreated.IsZero() {
		newSince = maxCreated
	}
	if perr := r.q.UpsertSyncCursor(ctx, sqlitedb.UpsertSyncCursorParams{
		ConnectionID: conn.ID,
		Resource:     resource,
		Cursor:       formatCursor(newSince, page1.ETag),
		UpdatedAt:    r.now().UTC(),
	}); perr != nil {
		return items, fmt.Errorf("upsert cursor: %w", perr)
	}
	return items, nil
}

// processPage upserts deploys whose created_at > since for one page.
// Returns (items_written, max_created_seen, saw_old, err). saw_old=true
// when any deploy in this page has created_at <= since — the caller uses
// this to break the paging loop.
func (r *Runner) processPage(ctx context.Context, repoID int64, page *ghdeployments.Page, since time.Time) (int64, time.Time, bool, error) {
	var (
		items      int64
		maxCreated time.Time
		sawOld     bool
	)
	for _, d := range page.Deployments {
		if !d.CreatedAt.After(since) {
			sawOld = true
			continue
		}
		if err := r.q.UpsertDeployment(ctx, sqlitedb.UpsertDeploymentParams{
			GhID:        d.GHID,
			RepoID:      repoID,
			Environment: d.Environment,
			Ref:         d.Ref,
			Sha:         d.SHA,
			Status:      "",
			CreatedAt:   d.CreatedAt,
		}); err != nil {
			return items, maxCreated, sawOld, fmt.Errorf("upsert deployment %d: %w", d.GHID, err)
		}
		items++
		if d.CreatedAt.After(maxCreated) {
			maxCreated = d.CreatedAt
		}
	}
	return items, maxCreated, sawOld, nil
}

// loadCursor reads the composite cursor for a resource and splits it into
// (since, etag). Missing row → (conn.BackfillFrom, ""). Legacy single-
// component (bare RFC3339 with no `|`) parses as (since, "").
func (r *Runner) loadCursor(ctx context.Context, connectionID int64, resource string, backfillFrom time.Time) (time.Time, string, error) {
	cur, err := r.q.GetSyncCursor(ctx, sqlitedb.GetSyncCursorParams{
		ConnectionID: connectionID,
		Resource:     resource,
	})
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return backfillFrom, "", nil
	case err != nil:
		return time.Time{}, "", fmt.Errorf("get cursor: %w", err)
	}
	sinceStr, etag, _ := strings.Cut(cur.Cursor, "|")
	ts, perr := time.Parse(time.RFC3339Nano, sinceStr)
	if perr != nil {
		return time.Time{}, "", fmt.Errorf("parse cursor since %q: %w", sinceStr, perr)
	}
	return ts, etag, nil
}

func formatCursor(since time.Time, etag string) string {
	return since.UTC().Format(time.RFC3339Nano) + "|" + etag
}
