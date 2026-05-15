package prconvo

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/karnstack/tempo/internal/github"
	"github.com/karnstack/tempo/internal/github/prconvo"
	"github.com/karnstack/tempo/internal/github/prs"
	"github.com/karnstack/tempo/internal/ingest"
	"github.com/karnstack/tempo/internal/storage/sqlite/sqlitedb"
	"go.uber.org/zap"
)

// pageSize is the per-request connection size for reviews, review threads,
// and issue comments. 100 is GitHub's max and the prconvo fetcher's clamp.
const pageSize = 100

// Runner ingests PR sub-resources (reviews, review comments, issue
// comments) for one connection per call to Run.
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
func (*Runner) Name() string { return "prconvo" }

// Run implements ingest.Runner. The scheduler hands us a per-connection
// *github.Client; we own the per-repo, per-PR iteration.
func (r *Runner) Run(ctx context.Context, conn sqlitedb.Connection, gh *github.Client) (ingest.Outcome, error) {
	repos, err := r.q.ListReposByConnection(ctx, conn.ID)
	if err != nil {
		return ingest.Outcome{}, fmt.Errorf("list repos: %w", err)
	}

	f := prconvo.New(gh)
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
			r.log.Warn("ingest/prconvo: repo failed",
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

// syncRepo lists PRs whose updated_at > cursor, fetches reviews +
// review_comments + issue_comments for each, and writes a new cursor at
// the end (only if all PRs succeeded). A mid-repo failure returns early
// and the next tick re-fetches from the same since.
func (r *Runner) syncRepo(ctx context.Context, f *prconvo.Fetcher, conn sqlitedb.Connection, repo sqlitedb.Repo) (int64, error) {
	resource := "prconvo:" + repo.Owner + "/" + repo.Name

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

	prList, err := r.q.ListPullRequestsByRepoUpdatedSince(ctx, sqlitedb.ListPullRequestsByRepoUpdatedSinceParams{
		RepoID: repo.ID,
		Since:  since,
	})
	if err != nil {
		return 0, fmt.Errorf("list prs updated since: %w", err)
	}
	if len(prList) == 0 {
		return 0, nil
	}

	var (
		items      int64
		maxUpdated time.Time
	)
	for _, pr := range prList {
		n, perr := r.syncPR(ctx, f, conn, repo, pr)
		items += n
		if perr != nil {
			return items, fmt.Errorf("pr #%d: %w", pr.Number, perr)
		}
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

// syncPR fetches all three sub-resources for one PR, upserting rows as
// it goes. The page-loop for each sub-resource terminates on !HasNext.
// Returns items_written + nil on success; on any GraphQL or DB error,
// returns items_written_so_far + the error.
func (r *Runner) syncPR(ctx context.Context, f *prconvo.Fetcher, conn sqlitedb.Connection, repo sqlitedb.Repo, pr sqlitedb.PullRequest) (int64, error) {
	var items int64

	// Reviews.
	var after string
	for {
		page, perr := f.FetchReviews(ctx, repo.Owner, repo.Name, int(pr.Number), after, pageSize)
		if perr != nil {
			return items, fmt.Errorf("reviews: %w", perr)
		}
		for _, rv := range page.Reviews {
			if rv.SubmittedAt == nil {
				continue // PENDING review — submitted_at is NOT NULL in the schema.
			}
			authorID, perr := r.upsertActor(ctx, conn.TenantID, rv.Author, pr.UpdatedAt)
			if perr != nil {
				return items, fmt.Errorf("upsert reviewer %s: %w", rv.Author.Login, perr)
			}
			if perr := r.q.UpsertPullRequestReview(ctx, sqlitedb.UpsertPullRequestReviewParams{
				GhID:             rv.GHID,
				PrRepoID:         repo.ID,
				PrNumber:         pr.Number,
				ReviewerGhUserID: authorID,
				State:            rv.State,
				SubmittedAt:      *rv.SubmittedAt,
			}); perr != nil {
				return items, fmt.Errorf("upsert review %d: %w", rv.GHID, perr)
			}
			items++
		}
		if !page.HasNext {
			break
		}
		after = page.EndCursor
	}

	// Review comments.
	after = ""
	for {
		page, perr := f.FetchReviewComments(ctx, repo.Owner, repo.Name, int(pr.Number), after, pageSize)
		if perr != nil {
			return items, fmt.Errorf("review comments: %w", perr)
		}
		if page.Truncated {
			r.log.Warn("ingest/prconvo: review thread truncated",
				zap.Int64("connection_id", conn.ID),
				zap.String("repo", repo.Owner+"/"+repo.Name),
				zap.Int64("pr_number", pr.Number),
			)
		}
		for _, c := range page.Comments {
			authorID, perr := r.upsertActor(ctx, conn.TenantID, c.Author, pr.UpdatedAt)
			if perr != nil {
				return items, fmt.Errorf("upsert review-comment author %s: %w", c.Author.Login, perr)
			}
			if perr := r.q.UpsertPullRequestReviewComment(ctx, sqlitedb.UpsertPullRequestReviewCommentParams{
				GhID:           c.GHID,
				PrRepoID:       repo.ID,
				PrNumber:       pr.Number,
				AuthorGhUserID: authorID,
				CreatedAt:      c.CreatedAt,
			}); perr != nil {
				return items, fmt.Errorf("upsert review comment %d: %w", c.GHID, perr)
			}
			items++
		}
		if !page.HasNext {
			break
		}
		after = page.EndCursor
	}

	// Issue comments.
	after = ""
	for {
		page, perr := f.FetchIssueComments(ctx, repo.Owner, repo.Name, int(pr.Number), after, pageSize)
		if perr != nil {
			return items, fmt.Errorf("issue comments: %w", perr)
		}
		for _, c := range page.Comments {
			authorID, perr := r.upsertActor(ctx, conn.TenantID, c.Author, pr.UpdatedAt)
			if perr != nil {
				return items, fmt.Errorf("upsert issue-comment author %s: %w", c.Author.Login, perr)
			}
			if perr := r.q.UpsertPullRequestIssueComment(ctx, sqlitedb.UpsertPullRequestIssueCommentParams{
				GhID:           c.GHID,
				PrRepoID:       repo.ID,
				PrNumber:       pr.Number,
				AuthorGhUserID: authorID,
				CreatedAt:      c.CreatedAt,
			}); perr != nil {
				return items, fmt.Errorf("upsert issue comment %d: %w", c.GHID, perr)
			}
			items++
		}
		if !page.HasNext {
			break
		}
		after = page.EndCursor
	}

	return items, nil
}

// upsertActor returns 0 for Ghost (deleted) authors so the row's
// *_gh_user_id sentinel is unambiguous. Mirrors the prs runner's
// upsertAuthor: v1 has no FK on these columns; rollups bucket 0 as
// "unknown actor". `seen` is used as the gh_users.last_seen_at hint.
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
