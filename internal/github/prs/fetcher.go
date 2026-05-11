package prs

import (
	"context"
	"fmt"
	"time"

	"github.com/karnstack/tempo/internal/github"
)

const (
	maxPageSize     = 100
	defaultPageSize = 50
)

// PR is one pull request as we care about it for tempo's metrics. Field set
// mirrors the `pull_requests` table (see migration 0002_raw_events).
type PR struct {
	GHID      int64
	Number    int
	Title     string
	State     string // "OPEN" | "CLOSED" | "MERGED"
	Author    Author
	CreatedAt time.Time
	UpdatedAt time.Time
	MergedAt  *time.Time
	ClosedAt  *time.Time
	Additions int
	Deletions int
	BaseRef   string
	HeadRef   string
	Draft     bool
}

// Author is the actor that opened a PR. Type is the GraphQL `__typename` —
// "User", "Bot", "Mannequin", or other future actor types. When GitHub
// returns `author: null` (the account was deleted), Type is "Ghost" and
// GHID is 0.
type Author struct {
	GHID  int64
	Login string
	Type  string
}

// Page is one slice of the `repository.pullRequests` connection. Callers
// drive their own loop; on each page they get GHRaphQL's cursor fields and a
// `ReachedSince` signal to stop.
type Page struct {
	PRs       []PR
	HasNext   bool
	EndCursor string
	// ReachedSince is true when at least one returned node had
	// UpdatedAt <= since. Those nodes are dropped from PRs so the caller
	// doesn't see them; the flag tells the caller to stop paginating.
	ReachedSince bool
}

// Fetcher pages pull requests for a single repo. Stateless; safe to share.
type Fetcher struct{ c *github.Client }

// New wires a Fetcher to a *github.Client (which already owns rate limits +
// retries).
func New(c *github.Client) *Fetcher { return &Fetcher{c: c} }

const listQuery = `query($owner: String!, $repo: String!, $first: Int!, $after: String) {
  repository(owner: $owner, name: $repo) {
    pullRequests(first: $first, after: $after, orderBy: {field: UPDATED_AT, direction: DESC}) {
      pageInfo { hasNextPage endCursor }
      nodes {
        databaseId number title state
        createdAt updatedAt mergedAt closedAt
        additions deletions
        baseRefName headRefName
        isDraft
        author {
          __typename
          login
          ... on User { databaseId }
          ... on Bot { databaseId }
          ... on Mannequin { databaseId }
        }
      }
    }
  }
}`

// Fetch pulls one page of PRs ordered by `updatedAt` DESC. `after` is the
// GraphQL `endCursor` from the previous page (empty for the first page).
// `first` is clamped to [1, 100]; a non-positive value defaults to 50.
//
// If `since` is non-zero, PRs with `UpdatedAt <= since` are dropped from
// Page.PRs and Page.ReachedSince is set so the caller can stop after this
// page. A zero `since` disables the cutoff entirely.
//
// GraphQL application errors surface as *github.GraphQLError unchanged.
func (f *Fetcher) Fetch(ctx context.Context, owner, repo, after string, first int, since time.Time) (*Page, error) {
	switch {
	case first <= 0:
		first = defaultPageSize
	case first > maxPageSize:
		first = maxPageSize
	}
	vars := map[string]any{
		"owner": owner,
		"repo":  repo,
		"first": first,
		"after": nil,
	}
	if after != "" {
		vars["after"] = after
	}

	var raw rawPage
	if err := f.c.GraphQL(ctx, listQuery, vars, &raw); err != nil {
		return nil, fmt.Errorf("prs: fetch %s/%s: %w", owner, repo, err)
	}

	page := &Page{
		HasNext:   raw.Repository.PullRequests.PageInfo.HasNextPage,
		EndCursor: raw.Repository.PullRequests.PageInfo.EndCursor,
		PRs:       make([]PR, 0, len(raw.Repository.PullRequests.Nodes)),
	}
	for _, n := range raw.Repository.PullRequests.Nodes {
		if !since.IsZero() && !n.UpdatedAt.After(since) {
			page.ReachedSince = true
			continue
		}
		page.PRs = append(page.PRs, n.toPR())
	}
	return page, nil
}

type rawPage struct {
	Repository struct {
		PullRequests struct {
			PageInfo struct {
				HasNextPage bool   `json:"hasNextPage"`
				EndCursor   string `json:"endCursor"`
			} `json:"pageInfo"`
			Nodes []rawPR `json:"nodes"`
		} `json:"pullRequests"`
	} `json:"repository"`
}

type rawPR struct {
	DatabaseID  int64      `json:"databaseId"`
	Number      int        `json:"number"`
	Title       string     `json:"title"`
	State       string     `json:"state"`
	CreatedAt   time.Time  `json:"createdAt"`
	UpdatedAt   time.Time  `json:"updatedAt"`
	MergedAt    *time.Time `json:"mergedAt"`
	ClosedAt    *time.Time `json:"closedAt"`
	Additions   int        `json:"additions"`
	Deletions   int        `json:"deletions"`
	BaseRefName string     `json:"baseRefName"`
	HeadRefName string     `json:"headRefName"`
	IsDraft     bool       `json:"isDraft"`
	Author      *rawAuthor `json:"author"`
}

type rawAuthor struct {
	Typename   string `json:"__typename"`
	Login      string `json:"login"`
	DatabaseID int64  `json:"databaseId"`
}

func (r rawPR) toPR() PR {
	return PR{
		GHID:      r.DatabaseID,
		Number:    r.Number,
		Title:     r.Title,
		State:     r.State,
		Author:    r.Author.parse(),
		CreatedAt: r.CreatedAt,
		UpdatedAt: r.UpdatedAt,
		MergedAt:  r.MergedAt,
		ClosedAt:  r.ClosedAt,
		Additions: r.Additions,
		Deletions: r.Deletions,
		BaseRef:   r.BaseRefName,
		HeadRef:   r.HeadRefName,
		Draft:     r.IsDraft,
	}
}

func (r *rawAuthor) parse() Author {
	if r == nil {
		return Author{Type: "Ghost"}
	}
	return Author{GHID: r.DatabaseID, Login: r.Login, Type: r.Typename}
}
