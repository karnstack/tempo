package commits

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/karnstack/tempo/internal/github"
	ghcommits "github.com/karnstack/tempo/internal/github/commits"
	"github.com/karnstack/tempo/internal/github/prs"
	"github.com/karnstack/tempo/internal/ingest"
	"github.com/karnstack/tempo/internal/storage/sqlite/sqlitedb"
	"go.uber.org/zap"
)

// pageSize is the per-request page size on `GET .../commits`. 100 is
// GitHub's max and the commits fetcher's clamp.
const pageSize = 100

// Runner ingests default-branch commits for one connection per call to Run.
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
func (*Runner) Name() string { return "commits" }

// Run implements ingest.Runner. The scheduler hands us a per-connection
// *github.Client; we own the per-repo iteration.
func (r *Runner) Run(ctx context.Context, conn sqlitedb.Connection, gh *github.Client) (ingest.Outcome, error) {
	repos, err := r.q.ListReposByConnection(ctx, conn.ID)
	if err != nil {
		return ingest.Outcome{}, fmt.Errorf("list repos: %w", err)
	}

	f := ghcommits.New(gh)
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
			r.log.Warn("ingest/commits: repo failed",
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

// syncRepo pages through commits for one repo, upserts authors/committers
// and commit rows, and writes a new cursor at the end. A mid-page failure
// returns early and the next tick re-fetches from the same (since, etag).
//
// Cursor advance rules (see package doc):
//
//   - 304 NotModified → no cursor write.
//   - 200 OK, N>0 commits → (max(authoredAt), etag="").
//   - 200 OK, 0 commits → (since unchanged, etag refreshed from page 1).
func (r *Runner) syncRepo(ctx context.Context, f *ghcommits.Fetcher, conn sqlitedb.Connection, repo sqlitedb.Repo) (int64, error) {
	resource := "commits:" + repo.Owner + "/" + repo.Name

	since, etag, err := r.loadCursor(ctx, conn.ID, resource, conn.BackfillFrom)
	if err != nil {
		return 0, err
	}

	// Page 1 — the only page that carries a meaningful etag.
	page1, err := f.Fetch(ctx, repo.Owner, repo.Name, ghcommits.FetchOptions{
		Since:   since,
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
		items      int64
		maxAuthored time.Time
		page        = page1
	)
	for {
		n, m, perr := r.processPage(ctx, conn.TenantID, repo.ID, page)
		items += n
		if m.After(maxAuthored) {
			maxAuthored = m
		}
		if perr != nil {
			return items, perr
		}
		if !page.HasNext {
			break
		}
		next, perr := f.Fetch(ctx, repo.Owner, repo.Name, ghcommits.FetchOptions{
			Since:   since,
			PerPage: pageSize,
			Page:    page.NextPage,
		})
		if perr != nil {
			return items, perr
		}
		page = next
	}

	newSince := since
	newEtag := page1.ETag
	if !maxAuthored.IsZero() {
		newSince = maxAuthored
		newEtag = ""
	}
	if perr := r.q.UpsertSyncCursor(ctx, sqlitedb.UpsertSyncCursorParams{
		ConnectionID: conn.ID,
		Resource:     resource,
		Cursor:       formatCursor(newSince, newEtag),
		UpdatedAt:    r.now().UTC(),
	}); perr != nil {
		return items, fmt.Errorf("upsert cursor: %w", perr)
	}
	return items, nil
}

// processPage upserts authors/committers and commit rows for one page.
// Returns (items_written, max_authored_seen, err).
func (r *Runner) processPage(ctx context.Context, tenantID, repoID int64, page *ghcommits.Page) (int64, time.Time, error) {
	var (
		items       int64
		maxAuthored time.Time
	)
	for _, c := range page.Commits {
		authorID, err := r.upsertActor(ctx, tenantID, c.Author, c.AuthoredAt)
		if err != nil {
			return items, maxAuthored, fmt.Errorf("upsert author %s: %w", c.Author.Login, err)
		}
		committerID, err := r.upsertActor(ctx, tenantID, c.Committer, c.CommittedAt)
		if err != nil {
			return items, maxAuthored, fmt.Errorf("upsert committer %s: %w", c.Committer.Login, err)
		}
		if err := r.q.UpsertCommit(ctx, sqlitedb.UpsertCommitParams{
			RepoID:            repoID,
			Sha:               c.SHA,
			AuthorGhUserID:    authorID,
			CommitterGhUserID: committerID,
			AuthoredAt:        c.AuthoredAt,
			Additions:         0,
			Deletions:         0,
			Message:           c.Message,
		}); err != nil {
			return items, maxAuthored, fmt.Errorf("upsert commit %s: %w", c.SHA, err)
		}
		items++
		if c.AuthoredAt.After(maxAuthored) {
			maxAuthored = c.AuthoredAt
		}
	}
	return items, maxAuthored, nil
}

// upsertActor returns 0 for Ghost (deleted) authors/committers so the
// commit row's *_gh_user_id sentinel is unambiguous. v1 has no FK on these
// columns — rollups bucket 0 as "unknown actor". `seen` is used as the
// gh_users.last_seen_at hint.
func (r *Runner) upsertActor(ctx context.Context, tenantID int64, a prs.Author, seen time.Time) (int64, error) {
	if a.GHID == 0 {
		return 0, nil
	}
	user, err := r.q.UpsertGhUser(ctx, sqlitedb.UpsertGhUserParams{
		TenantID:   tenantID,
		GhID:       a.GHID,
		Login:      a.Login,
		LastSeenAt: &seen,
	})
	if err != nil {
		return 0, err
	}
	return user.ID, nil
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
