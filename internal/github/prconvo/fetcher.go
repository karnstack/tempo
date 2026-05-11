package prconvo

import (
	"context"
	"fmt"
	"time"

	"github.com/karnstack/tempo/internal/github"
	"github.com/karnstack/tempo/internal/github/prs"
)

const (
	maxPageSize     = 100
	defaultPageSize = 50
	// maxInlineComments is the per-thread cap on inlined review comments
	// (see reviewCommentsQuery). If a thread has more than this many
	// comments, ReviewCommentsPage.Truncated is set.
	maxInlineComments = 100
)

// Review is one submitted PR review as we care about it for tempo's
// metrics. Mirrors `pr_reviews(gh_id, reviewer_gh_user_id, state,
// submitted_at)` in the schema.
//
// State is the GraphQL enum verbatim: "APPROVED", "CHANGES_REQUESTED",
// "COMMENTED", "DISMISSED". A "PENDING" review may surface for tokens
// that own it; in that case SubmittedAt is nil.
type Review struct {
	GHID        int64
	State       string
	SubmittedAt *time.Time
	Author      prs.Author
}

// ReviewComment is one inline review comment on a PR diff. Mirrors
// `pr_review_comments(gh_id, author_gh_user_id, created_at)`.
type ReviewComment struct {
	GHID      int64
	Author    prs.Author
	CreatedAt time.Time
}

// IssueComment is one comment on the PR's conversation tab. Mirrors
// `pr_issue_comments(gh_id, author_gh_user_id, created_at)`.
type IssueComment struct {
	GHID      int64
	Author    prs.Author
	CreatedAt time.Time
}

// ReviewsPage is one slice of `pullRequest.reviews`.
type ReviewsPage struct {
	Reviews   []Review
	HasNext   bool
	EndCursor string
}

// ReviewCommentsPage is one slice of `pullRequest.reviewThreads`, with
// each thread's `comments(first: 100)` flattened into Comments. The
// EndCursor / HasNext refer to threads, not individual comments.
// Truncated is true when at least one thread had more than 100 inline
// comments and the overflow was dropped.
type ReviewCommentsPage struct {
	Comments  []ReviewComment
	HasNext   bool
	EndCursor string
	Truncated bool
}

// IssueCommentsPage is one slice of `pullRequest.comments`.
type IssueCommentsPage struct {
	Comments  []IssueComment
	HasNext   bool
	EndCursor string
}

// Fetcher pages per-PR sub-resources (reviews, review comments, issue
// comments). Stateless; safe to share.
type Fetcher struct{ c *github.Client }

// New wires a Fetcher to a *github.Client (which already owns rate
// limits + retries).
func New(c *github.Client) *Fetcher { return &Fetcher{c: c} }

const reviewsQuery = `query($owner: String!, $repo: String!, $number: Int!, $first: Int!, $after: String) {
  repository(owner: $owner, name: $repo) {
    pullRequest(number: $number) {
      reviews(first: $first, after: $after) {
        pageInfo { hasNextPage endCursor }
        nodes {
          databaseId state submittedAt
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
  }
}`

const reviewCommentsQuery = `query($owner: String!, $repo: String!, $number: Int!, $first: Int!, $after: String) {
  repository(owner: $owner, name: $repo) {
    pullRequest(number: $number) {
      reviewThreads(first: $first, after: $after) {
        pageInfo { hasNextPage endCursor }
        nodes {
          comments(first: 100) {
            pageInfo { hasNextPage }
            nodes {
              databaseId createdAt
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
      }
    }
  }
}`

const issueCommentsQuery = `query($owner: String!, $repo: String!, $number: Int!, $first: Int!, $after: String) {
  repository(owner: $owner, name: $repo) {
    pullRequest(number: $number) {
      comments(first: $first, after: $after) {
        pageInfo { hasNextPage endCursor }
        nodes {
          databaseId createdAt
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
  }
}`

// FetchReviews pages submitted reviews on a PR ordered by GitHub's
// default (ASC by submission time). `after` is the GraphQL `endCursor`
// from the previous page (empty for the first). `first` is clamped to
// [1,100]; <=0 defaults to 50.
func (f *Fetcher) FetchReviews(ctx context.Context, owner, repo string, number int, after string, first int) (*ReviewsPage, error) {
	vars := buildVars(owner, repo, number, after, clampFirst(first))

	var raw rawReviewsPage
	if err := f.c.GraphQL(ctx, reviewsQuery, vars, &raw); err != nil {
		return nil, fmt.Errorf("prconvo: fetch reviews %s/%s#%d: %w", owner, repo, number, err)
	}

	conn := raw.Repository.PullRequest.Reviews
	page := &ReviewsPage{
		HasNext:   conn.PageInfo.HasNextPage,
		EndCursor: conn.PageInfo.EndCursor,
		Reviews:   make([]Review, 0, len(conn.Nodes)),
	}
	for _, n := range conn.Nodes {
		page.Reviews = append(page.Reviews, Review{
			GHID:        n.DatabaseID,
			State:       n.State,
			SubmittedAt: n.SubmittedAt,
			Author:      n.Author.parse(),
		})
	}
	return page, nil
}

// FetchReviewComments pages inline diff comments on a PR by walking
// `reviewThreads` and flattening each thread's inlined comments(first:
// 100). EndCursor / HasNext refer to threads. If any thread had more
// than 100 comments, ReviewCommentsPage.Truncated is set.
func (f *Fetcher) FetchReviewComments(ctx context.Context, owner, repo string, number int, after string, first int) (*ReviewCommentsPage, error) {
	vars := buildVars(owner, repo, number, after, clampFirst(first))

	var raw rawReviewCommentsPage
	if err := f.c.GraphQL(ctx, reviewCommentsQuery, vars, &raw); err != nil {
		return nil, fmt.Errorf("prconvo: fetch review comments %s/%s#%d: %w", owner, repo, number, err)
	}

	conn := raw.Repository.PullRequest.ReviewThreads
	page := &ReviewCommentsPage{
		HasNext:   conn.PageInfo.HasNextPage,
		EndCursor: conn.PageInfo.EndCursor,
		Comments:  make([]ReviewComment, 0, len(conn.Nodes)),
	}
	for _, thread := range conn.Nodes {
		if thread.Comments.PageInfo.HasNextPage {
			page.Truncated = true
		}
		for _, c := range thread.Comments.Nodes {
			page.Comments = append(page.Comments, ReviewComment{
				GHID:      c.DatabaseID,
				Author:    c.Author.parse(),
				CreatedAt: c.CreatedAt,
			})
		}
	}
	return page, nil
}

// FetchIssueComments pages PR conversation-tab comments (GraphQL
// `pullRequest.comments`) — flat connection, cursor-driven.
func (f *Fetcher) FetchIssueComments(ctx context.Context, owner, repo string, number int, after string, first int) (*IssueCommentsPage, error) {
	vars := buildVars(owner, repo, number, after, clampFirst(first))

	var raw rawIssueCommentsPage
	if err := f.c.GraphQL(ctx, issueCommentsQuery, vars, &raw); err != nil {
		return nil, fmt.Errorf("prconvo: fetch issue comments %s/%s#%d: %w", owner, repo, number, err)
	}

	conn := raw.Repository.PullRequest.Comments
	page := &IssueCommentsPage{
		HasNext:   conn.PageInfo.HasNextPage,
		EndCursor: conn.PageInfo.EndCursor,
		Comments:  make([]IssueComment, 0, len(conn.Nodes)),
	}
	for _, n := range conn.Nodes {
		page.Comments = append(page.Comments, IssueComment{
			GHID:      n.DatabaseID,
			Author:    n.Author.parse(),
			CreatedAt: n.CreatedAt,
		})
	}
	return page, nil
}

func clampFirst(first int) int {
	switch {
	case first <= 0:
		return defaultPageSize
	case first > maxPageSize:
		return maxPageSize
	default:
		return first
	}
}

func buildVars(owner, repo string, number int, after string, first int) map[string]any {
	vars := map[string]any{
		"owner":  owner,
		"repo":   repo,
		"number": number,
		"first":  first,
		"after":  nil,
	}
	if after != "" {
		vars["after"] = after
	}
	return vars
}

type rawPageInfo struct {
	HasNextPage bool   `json:"hasNextPage"`
	EndCursor   string `json:"endCursor"`
}

type rawAuthor struct {
	Typename   string `json:"__typename"`
	Login      string `json:"login"`
	DatabaseID int64  `json:"databaseId"`
}

func (r *rawAuthor) parse() prs.Author {
	if r == nil {
		return prs.Author{Type: "Ghost"}
	}
	return prs.Author{GHID: r.DatabaseID, Login: r.Login, Type: r.Typename}
}

type rawReviewsPage struct {
	Repository struct {
		PullRequest struct {
			Reviews struct {
				PageInfo rawPageInfo `json:"pageInfo"`
				Nodes    []rawReview `json:"nodes"`
			} `json:"reviews"`
		} `json:"pullRequest"`
	} `json:"repository"`
}

type rawReview struct {
	DatabaseID  int64      `json:"databaseId"`
	State       string     `json:"state"`
	SubmittedAt *time.Time `json:"submittedAt"`
	Author      *rawAuthor `json:"author"`
}

type rawReviewCommentsPage struct {
	Repository struct {
		PullRequest struct {
			ReviewThreads struct {
				PageInfo rawPageInfo  `json:"pageInfo"`
				Nodes    []rawThread  `json:"nodes"`
			} `json:"reviewThreads"`
		} `json:"pullRequest"`
	} `json:"repository"`
}

type rawThread struct {
	Comments struct {
		PageInfo struct {
			HasNextPage bool `json:"hasNextPage"`
		} `json:"pageInfo"`
		Nodes []rawReviewComment `json:"nodes"`
	} `json:"comments"`
}

type rawReviewComment struct {
	DatabaseID int64      `json:"databaseId"`
	CreatedAt  time.Time  `json:"createdAt"`
	Author     *rawAuthor `json:"author"`
}

type rawIssueCommentsPage struct {
	Repository struct {
		PullRequest struct {
			Comments struct {
				PageInfo rawPageInfo       `json:"pageInfo"`
				Nodes    []rawIssueComment `json:"nodes"`
			} `json:"comments"`
		} `json:"pullRequest"`
	} `json:"repository"`
}

type rawIssueComment struct {
	DatabaseID int64      `json:"databaseId"`
	CreatedAt  time.Time  `json:"createdAt"`
	Author     *rawAuthor `json:"author"`
}
