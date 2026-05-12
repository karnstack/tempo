package orgrepos

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/karnstack/tempo/internal/github"
)

const (
	maxPageSize     = 100
	defaultPageSize = 50
)

// Repo is one entry from `GET /orgs/{org}/repos`. Fields are the
// intersection of the spec's `repos` table (gh_id, owner, name,
// default_branch, archived) and the worker's filter inputs (fork,
// private). Owner is the parent org's login.
type Repo struct {
	GHID          int64
	Owner         string
	Name          string
	DefaultBranch string
	Archived      bool
	Fork          bool
	Private       bool
}

// FetchOptions parameterises a single Fetch call. The endpoint accepts
// only a `type` filter plus pagination / conditional headers.
type FetchOptions struct {
	// Type maps to the `type` query param: "all" (server default when
	// empty), "public", "private", "forks", "sources", "member". We
	// don't validate; invalid values surface as *github.HTTPError 422.
	Type string

	// Page is the 1-indexed page number. Zero or negative becomes 1.
	Page int

	// PerPage is clamped to [1,100]. Zero or negative defaults to 50.
	PerPage int

	// ETag is sent as `If-None-Match` to enable conditional polling.
	// Silently dropped when Page > 1 — each paginated URL has its own
	// server-side ETag and only the first page is worth caching.
	ETag string
}

// Page is one slice of an org's repos list, plus the cursor signals
// the caller needs to drive pagination and ETag-based conditional
// polling.
type Page struct {
	Repos []Repo

	// HasNext is true iff the response Link header carried a rel="next"
	// entry. NextPage is the page number to request next (== current+1)
	// when HasNext, else 0.
	HasNext  bool
	NextPage int

	// NotModified is true when the server returned 304. In that case
	// Repos is empty, HasNext is false, NextPage is 0, and ETag echoes
	// either the server's ETag (when present) or the caller's input.
	NotModified bool

	// ETag is the server-returned ETag, suitable for storing in the
	// caller's cursor and replaying on the next poll.
	ETag string
}

// Fetcher pages repos for a single org. Stateless; safe to share.
type Fetcher struct{ c *github.Client }

// New wires a Fetcher to a *github.Client (which already owns rate
// limits + retries).
func New(c *github.Client) *Fetcher { return &Fetcher{c: c} }

// Fetch pulls one page of repos for org. See FetchOptions for parameter
// semantics and Page for response shape.
//
// HTTP errors propagate as *github.HTTPError unchanged (4xx via the
// inner client; we don't wrap). A 304 is NOT an error — it surfaces as
// Page.NotModified.
func (f *Fetcher) Fetch(ctx context.Context, org string, opts FetchOptions) (*Page, error) {
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
	if opts.Type != "" {
		q.Set("type", opts.Type)
	}

	path := fmt.Sprintf("/orgs/%s/repos?%s", org, q.Encode())

	var headers http.Header
	if page <= 1 && opts.ETag != "" {
		headers = http.Header{"If-None-Match": []string{opts.ETag}}
	}

	resp, err := f.c.REST(ctx, http.MethodGet, path, nil, headers)
	if err != nil {
		return nil, fmt.Errorf("orgrepos: fetch %s: %w", org, err)
	}

	out := &Page{ETag: resp.ETag}
	if resp.Status == http.StatusNotModified {
		out.NotModified = true
		if out.ETag == "" {
			out.ETag = opts.ETag
		}
		return out, nil
	}

	var raw []rawRepo
	if len(resp.Body) > 0 {
		if err := json.Unmarshal(resp.Body, &raw); err != nil {
			return nil, fmt.Errorf("orgrepos: decode %s: %w", org, err)
		}
	}
	out.Repos = make([]Repo, 0, len(raw))
	for _, n := range raw {
		out.Repos = append(out.Repos, n.toRepo())
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

// rawRepo drops fields tempo doesn't need (description, html_url,
// pushed_at, language, license, topics, the full owner object, etc.).
// Only `owner.login` is kept.
type rawRepo struct {
	ID            int64  `json:"id"`
	Name          string `json:"name"`
	DefaultBranch string `json:"default_branch"`
	Archived      bool   `json:"archived"`
	Fork          bool   `json:"fork"`
	Private       bool   `json:"private"`
	Owner         struct {
		Login string `json:"login"`
	} `json:"owner"`
}

func (r rawRepo) toRepo() Repo {
	return Repo{
		GHID:          r.ID,
		Owner:         r.Owner.Login,
		Name:          r.Name,
		DefaultBranch: r.DefaultBranch,
		Archived:      r.Archived,
		Fork:          r.Fork,
		Private:       r.Private,
	}
}
