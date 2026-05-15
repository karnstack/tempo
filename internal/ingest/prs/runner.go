package prs

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/karnstack/tempo/internal/github"
	ghprs "github.com/karnstack/tempo/internal/github/prs"
	"github.com/karnstack/tempo/internal/ingest"
	"github.com/karnstack/tempo/internal/storage/sqlite/sqlitedb"
	"go.uber.org/zap"
)

// Runner ingests pull requests for one connection per call to Run.
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
func (*Runner) Name() string { return "prs" }

// Run implements ingest.Runner. The scheduler hands us a per-connection
// *github.Client; we own the per-repo iteration.
func (r *Runner) Run(ctx context.Context, conn sqlitedb.Connection, gh *github.Client) (ingest.Outcome, error) {
	repos, err := r.q.ListReposByConnection(ctx, conn.ID)
	if err != nil {
		return ingest.Outcome{}, fmt.Errorf("list repos: %w", err)
	}

	f := ghprs.New(gh)
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
			r.log.Warn("ingest/prs: repo failed",
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
	if rem, ok := gh.GraphQLRemaining(); ok {
		v := int64(rem)
		out.RateLimitRemaining = &v
	}
	return out, firstErr
}

// syncRepo fetches one page of PRs for one repo, upserts authors + PRs, and
// (if maxUpdated advanced) writes the new cursor. Step 4 will turn the single
// Fetch into a page loop.
func (r *Runner) syncRepo(ctx context.Context, f *ghprs.Fetcher, conn sqlitedb.Connection, repo sqlitedb.Repo) (int64, error) {
	resource := "prs:" + repo.Owner + "/" + repo.Name

	since := conn.BackfillFrom
	cur, err := r.q.GetSyncCursor(ctx, sqlitedb.GetSyncCursorParams{
		ConnectionID: conn.ID,
		Resource:     resource,
	})
	switch {
	case errors.Is(err, sql.ErrNoRows):
		// no cursor yet → use BackfillFrom
	case err != nil:
		return 0, fmt.Errorf("get cursor: %w", err)
	default:
		ts, perr := time.Parse(time.RFC3339Nano, cur.Cursor)
		if perr != nil {
			return 0, fmt.Errorf("parse cursor %q: %w", cur.Cursor, perr)
		}
		since = ts
	}

	page, err := f.Fetch(ctx, repo.Owner, repo.Name, "", 100, since)
	if err != nil {
		return 0, err
	}

	var (
		items      int64
		maxUpdated time.Time
	)
	for _, pr := range page.PRs {
		authorID, perr := r.upsertAuthor(ctx, conn.TenantID, pr)
		if perr != nil {
			return items, fmt.Errorf("upsert author %s: %w", pr.Author.Login, perr)
		}
		if perr := r.upsertPR(ctx, repo.ID, pr, authorID); perr != nil {
			return items, fmt.Errorf("upsert pr #%d: %w", pr.Number, perr)
		}
		items++
		if pr.UpdatedAt.After(maxUpdated) {
			maxUpdated = pr.UpdatedAt
		}
	}

	if !maxUpdated.IsZero() {
		if perr := r.q.UpsertSyncCursor(ctx, sqlitedb.UpsertSyncCursorParams{
			ConnectionID: conn.ID,
			Resource:     resource,
			Cursor:       maxUpdated.UTC().Format(time.RFC3339Nano),
			UpdatedAt:    r.now().UTC(),
		}); perr != nil {
			return items, fmt.Errorf("upsert cursor: %w", perr)
		}
	}
	return items, nil
}

// upsertAuthor returns 0 for Ghost (deleted) authors so the PR row's
// author_gh_user_id sentinel is unambiguous. v1 has no FK on this column —
// rollups bucket 0 as "unknown actor".
func (r *Runner) upsertAuthor(ctx context.Context, tenantID int64, pr ghprs.PR) (int64, error) {
	if pr.Author.GHID == 0 {
		return 0, nil
	}
	seen := pr.UpdatedAt
	user, err := r.q.UpsertGhUser(ctx, sqlitedb.UpsertGhUserParams{
		TenantID:   tenantID,
		GhID:       pr.Author.GHID,
		Login:      pr.Author.Login,
		LastSeenAt: &seen,
	})
	if err != nil {
		return 0, err
	}
	return user.ID, nil
}

func (r *Runner) upsertPR(ctx context.Context, repoID int64, pr ghprs.PR, authorID int64) error {
	var draft int64
	if pr.Draft {
		draft = 1
	}
	return r.q.UpsertPullRequest(ctx, sqlitedb.UpsertPullRequestParams{
		RepoID:         repoID,
		Number:         int64(pr.Number),
		GhID:           pr.GHID,
		AuthorGhUserID: authorID,
		State:          pr.State,
		Title:          pr.Title,
		CreatedAt:      pr.CreatedAt,
		MergedAt:       pr.MergedAt,
		ClosedAt:       pr.ClosedAt,
		Additions:      int64(pr.Additions),
		Deletions:      int64(pr.Deletions),
		BaseRef:        pr.BaseRef,
		HeadRef:        pr.HeadRef,
		Draft:          draft,
	})
}
