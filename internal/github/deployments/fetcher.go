package deployments

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

// Deployment is one entry from `GET /repos/{owner}/{repo}/deployments`.
// Creator is the GitHub-mapped user that triggered the deploy
// (Type ∈ {"User","Bot","Mannequin","Ghost"}; "Ghost" means the JSON
// `creator` was null — account deleted). Description can be empty: GitHub
// returns either "" or `null` and we treat both as zero-value string.
//
// The deployment_status (success / failure / in_progress / inactive) is
// NOT populated here — the list endpoint omits it. See this package's
// doc.go for the rationale.
type Deployment struct {
	GHID        int64
	SHA         string
	Ref         string
	Task        string
	Environment string
	Description string
	Creator     prs.Author
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// FetchOptions parameterises a single Fetch call. Zero values mean "omit"
// for SHA / Ref / Environment / ETag, "default" for Page / PerPage.
type FetchOptions struct {
	// SHA filters to deployments matching this commit SHA. Empty omits.
	SHA string

	// Ref filters to deployments matching this branch/tag. Empty omits.
	Ref string

	// Environment filters to deployments in this environment
	// (e.g. "production", "staging"). Empty omits.
	Environment string

	// Page is the 1-indexed page number. Zero or negative becomes 1.
	Page int

	// PerPage is clamped to [1,100]. Zero or negative defaults to 50.
	PerPage int

	// ETag is sent as `If-None-Match` to enable conditional polling.
	// Silently dropped when Page > 1 — each paginated URL has its own
	// server-side ETag and only the first page is worth caching.
	ETag string
}

// Page is one slice of the deployments list, plus the cursor signals the
// caller needs to drive pagination and ETag-based conditional polling.
type Page struct {
	Deployments []Deployment

	// HasNext is true iff the response Link header carried a rel="next"
	// entry. NextPage is the page number to request next (== current+1)
	// when HasNext, else 0.
	HasNext  bool
	NextPage int

	// NotModified is true when the server returned 304. In that case
	// Deployments is empty, HasNext is false, NextPage is 0, and ETag
	// echoes either the server's ETag (when present) or the caller's
	// input.
	NotModified bool

	// ETag is the server-returned ETag, suitable for storing in the
	// caller's cursor and replaying on the next poll.
	ETag string
}

// Fetcher pages deployments for a single repo. Stateless; safe to share.
type Fetcher struct{ c *github.Client }

// New wires a Fetcher to a *github.Client (which already owns rate limits +
// retries).
func New(c *github.Client) *Fetcher { return &Fetcher{c: c} }

// Fetch pulls one page of deployments. See FetchOptions for parameter
// semantics and Page for response shape.
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
	if opts.SHA != "" {
		q.Set("sha", opts.SHA)
	}
	if opts.Ref != "" {
		q.Set("ref", opts.Ref)
	}
	if opts.Environment != "" {
		q.Set("environment", opts.Environment)
	}

	path := fmt.Sprintf("/repos/%s/%s/deployments?%s", owner, repo, q.Encode())

	var headers http.Header
	if page <= 1 && opts.ETag != "" {
		headers = http.Header{"If-None-Match": []string{opts.ETag}}
	}

	resp, err := f.c.REST(ctx, http.MethodGet, path, nil, headers)
	if err != nil {
		return nil, fmt.Errorf("deployments: fetch %s/%s: %w", owner, repo, err)
	}

	out := &Page{ETag: resp.ETag}
	if resp.Status == http.StatusNotModified {
		out.NotModified = true
		if out.ETag == "" {
			out.ETag = opts.ETag
		}
		return out, nil
	}

	var raw []rawDeployment
	if len(resp.Body) > 0 {
		if err := json.Unmarshal(resp.Body, &raw); err != nil {
			return nil, fmt.Errorf("deployments: decode %s/%s: %w", owner, repo, err)
		}
	}
	out.Deployments = make([]Deployment, 0, len(raw))
	for _, n := range raw {
		out.Deployments = append(out.Deployments, n.toDeployment())
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

// rawDeployment intentionally drops `payload`, `statuses_url`, etc.
// `description` decodes as "" when JSON sends null (Go's encoding/json
// leaves zero values untouched on null), so both "" and null collapse to
// the same Description.
type rawDeployment struct {
	ID          int64     `json:"id"`
	SHA         string    `json:"sha"`
	Ref         string    `json:"ref"`
	Task        string    `json:"task"`
	Environment string    `json:"environment"`
	Description string    `json:"description"`
	Creator     *rawActor `json:"creator"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type rawActor struct {
	Login string `json:"login"`
	ID    int64  `json:"id"`
	Type  string `json:"type"`
}

func (r rawDeployment) toDeployment() Deployment {
	return Deployment{
		GHID:        r.ID,
		SHA:         r.SHA,
		Ref:         r.Ref,
		Task:        r.Task,
		Environment: r.Environment,
		Description: r.Description,
		Creator:     parseActor(r.Creator),
		CreatedAt:   r.CreatedAt,
		UpdatedAt:   r.UpdatedAt,
	}
}

func parseActor(r *rawActor) prs.Author {
	if r == nil {
		return prs.Author{Type: "Ghost"}
	}
	return prs.Author{GHID: r.ID, Login: r.Login, Type: r.Type}
}
