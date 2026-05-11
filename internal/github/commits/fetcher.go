package commits

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/karnstack/tempo/internal/github"
	"github.com/karnstack/tempo/internal/github/prs"
)

const (
	maxPageSize     = 100
	defaultPageSize = 50
)

// Commit is one commit from `GET /repos/{owner}/{repo}/commits`. Author and
// Committer are the GitHub-mapped users (Type ∈ {"User","Bot","Mannequin",
// "Ghost"}; "Ghost" means the JSON `author`/`committer` was null — account
// deleted or email unmatched). AuthoredAt and CommittedAt come from
// `commit.author.date` / `commit.committer.date` and may differ when a
// commit is rebased or cherry-picked.
//
// Additions and deletions are NOT populated — the list endpoint omits them;
// see this package's doc.go for the rationale.
type Commit struct {
	SHA         string
	Message     string
	Author      prs.Author
	Committer   prs.Author
	AuthoredAt  time.Time
	CommittedAt time.Time
}

// FetchOptions parameterises a single Fetch call. Zero values mean "omit"
// for SHA / Since / ETag, "default" for Page / PerPage.
type FetchOptions struct {
	// SHA selects a branch, tag, or commit SHA. Empty defers to the repo's
	// default branch (server-side default).
	SHA string

	// Since filters commits with author-date strictly after this timestamp.
	// Zero omits the `since` query param entirely.
	Since time.Time

	// Page is the 1-indexed page number. Zero or negative becomes 1.
	Page int

	// PerPage is clamped to [1,100]. Zero or negative defaults to 50.
	PerPage int

	// ETag is sent as `If-None-Match` to enable conditional polling.
	// Silently dropped when Page > 1 — each paginated URL has its own
	// server-side ETag and only the first page is worth caching.
	ETag string
}

// Page is one slice of the commits list, plus the cursor signals the caller
// needs to drive pagination and ETag-based conditional polling.
type Page struct {
	Commits []Commit

	// HasNext is true iff the response Link header carried a rel="next"
	// entry. NextPage is the page number to request next (== current+1)
	// when HasNext, else 0.
	HasNext  bool
	NextPage int

	// NotModified is true when the server returned 304. In that case
	// Commits is empty, HasNext is false, NextPage is 0, and ETag echoes
	// either the server's ETag (when present) or the caller's input.
	NotModified bool

	// ETag is the server-returned ETag, suitable for storing in the
	// caller's cursor and replaying on the next poll.
	ETag string
}

// Fetcher pages commits for a single repo. Stateless; safe to share.
type Fetcher struct{ c *github.Client }

// New wires a Fetcher to a *github.Client (which already owns rate limits +
// retries).
func New(c *github.Client) *Fetcher { return &Fetcher{c: c} }

// Fetch pulls one page of commits. See FetchOptions for parameter semantics
// and Page for response shape.
//
// HTTP errors propagate as *github.HTTPError unchanged (4xx via the inner
// client; we don't wrap). A 304 is NOT an error — it surfaces as
// Page.NotModified.
func (f *Fetcher) Fetch(ctx context.Context, owner, repo string, opts FetchOptions) (*Page, error) {
	page := opts.Page
	if page <= 0 {
		page = 1
	}
	perPage := opts.PerPage
	switch {
	case perPage <= 0:
		perPage = defaultPageSize
	case perPage > maxPageSize:
		perPage = maxPageSize
	}

	q := url.Values{}
	q.Set("page", strconv.Itoa(page))
	q.Set("per_page", strconv.Itoa(perPage))
	if !opts.Since.IsZero() {
		q.Set("since", opts.Since.UTC().Format(time.RFC3339))
	}
	if opts.SHA != "" {
		q.Set("sha", opts.SHA)
	}

	path := fmt.Sprintf("/repos/%s/%s/commits?%s", owner, repo, q.Encode())

	var headers http.Header
	if page <= 1 && opts.ETag != "" {
		headers = http.Header{"If-None-Match": []string{opts.ETag}}
	}

	resp, err := f.c.REST(ctx, http.MethodGet, path, nil, headers)
	if err != nil {
		return nil, fmt.Errorf("commits: fetch %s/%s: %w", owner, repo, err)
	}

	out := &Page{ETag: resp.ETag}
	if resp.Status == http.StatusNotModified {
		out.NotModified = true
		if out.ETag == "" {
			out.ETag = opts.ETag
		}
		return out, nil
	}

	var raw []rawCommit
	if len(resp.Body) > 0 {
		if err := json.Unmarshal(resp.Body, &raw); err != nil {
			return nil, fmt.Errorf("commits: decode %s/%s: %w", owner, repo, err)
		}
	}
	out.Commits = make([]Commit, 0, len(raw))
	for _, n := range raw {
		out.Commits = append(out.Commits, n.toCommit())
	}
	if hasNextLink(resp.Headers.Get("Link")) {
		out.HasNext = true
		out.NextPage = page + 1
	}
	return out, nil
}

// hasNextLink scans a GitHub Link header for a rel="next" entry.
// Header form: `<url1>; rel="next", <url2>; rel="last"`.
func hasNextLink(h string) bool {
	if h == "" {
		return false
	}
	for _, part := range strings.Split(h, ",") {
		if strings.Contains(part, `rel="next"`) {
			return true
		}
	}
	return false
}

type rawCommit struct {
	SHA    string `json:"sha"`
	Commit struct {
		Message string `json:"message"`
		Author  struct {
			Date time.Time `json:"date"`
		} `json:"author"`
		Committer struct {
			Date time.Time `json:"date"`
		} `json:"committer"`
	} `json:"commit"`
	Author    *rawActor `json:"author"`
	Committer *rawActor `json:"committer"`
}

type rawActor struct {
	Login string `json:"login"`
	ID    int64  `json:"id"`
	Type  string `json:"type"`
}

func (r rawCommit) toCommit() Commit {
	return Commit{
		SHA:         r.SHA,
		Message:     r.Commit.Message,
		Author:      parseActor(r.Author),
		Committer:   parseActor(r.Committer),
		AuthoredAt:  r.Commit.Author.Date,
		CommittedAt: r.Commit.Committer.Date,
	}
}

func parseActor(r *rawActor) prs.Author {
	if r == nil {
		return prs.Author{Type: "Ghost"}
	}
	return prs.Author{GHID: r.ID, Login: r.Login, Type: r.Type}
}
