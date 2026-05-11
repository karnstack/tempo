package releases

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

// Release is one entry from `GET /repos/{owner}/{repo}/releases`. Author
// is the GitHub-mapped user that cut the release (Type ∈ {"User","Bot",
// "Mannequin","Ghost"}; "Ghost" means JSON `author` was null — account
// deleted). PublishedAt is zero when the release is a draft (GitHub
// returns `published_at: null` for unpublished drafts).
type Release struct {
	GHID            int64
	TagName         string
	Name            string
	Draft           bool
	Prerelease      bool
	TargetCommitish string
	Body            string
	Author          prs.Author
	CreatedAt       time.Time
	PublishedAt     time.Time // zero when Draft && unpublished
}

// FetchOptions parameterises a single Fetch call. The releases endpoint
// has no query-level filters; only pagination and ETag.
type FetchOptions struct {
	// Page is the 1-indexed page number. Zero or negative becomes 1.
	Page int

	// PerPage is clamped to [1,100]. Zero or negative defaults to 50.
	PerPage int

	// ETag is sent as `If-None-Match` to enable conditional polling.
	// Silently dropped when Page > 1 — each paginated URL has its own
	// server-side ETag and only the first page is worth caching.
	ETag string
}

// Page is one slice of the releases list, plus the cursor signals the
// caller needs to drive pagination and ETag-based conditional polling.
type Page struct {
	Releases []Release

	// HasNext is true iff the response Link header carried a rel="next"
	// entry. NextPage is the page number to request next (== current+1)
	// when HasNext, else 0.
	HasNext  bool
	NextPage int

	// NotModified is true when the server returned 304. In that case
	// Releases is empty, HasNext is false, NextPage is 0, and ETag echoes
	// either the server's ETag (when present) or the caller's input.
	NotModified bool

	// ETag is the server-returned ETag, suitable for storing in the
	// caller's cursor and replaying on the next poll.
	ETag string
}

// Fetcher pages releases for a single repo. Stateless; safe to share.
type Fetcher struct{ c *github.Client }

// New wires a Fetcher to a *github.Client (which already owns rate limits
// + retries).
func New(c *github.Client) *Fetcher { return &Fetcher{c: c} }

// Fetch pulls one page of releases. See FetchOptions for parameter
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

	path := fmt.Sprintf("/repos/%s/%s/releases?%s", owner, repo, q.Encode())

	var headers http.Header
	if page <= 1 && opts.ETag != "" {
		headers = http.Header{"If-None-Match": []string{opts.ETag}}
	}

	resp, err := f.c.REST(ctx, http.MethodGet, path, nil, headers)
	if err != nil {
		return nil, fmt.Errorf("releases: fetch %s/%s: %w", owner, repo, err)
	}

	out := &Page{ETag: resp.ETag}
	if resp.Status == http.StatusNotModified {
		out.NotModified = true
		if out.ETag == "" {
			out.ETag = opts.ETag
		}
		return out, nil
	}

	var raw []rawRelease
	if len(resp.Body) > 0 {
		if err := json.Unmarshal(resp.Body, &raw); err != nil {
			return nil, fmt.Errorf("releases: decode %s/%s: %w", owner, repo, err)
		}
	}
	out.Releases = make([]Release, 0, len(raw))
	for _, n := range raw {
		out.Releases = append(out.Releases, n.toRelease())
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

// rawRelease drops fields tempo doesn't need (tarball_url, zipball_url,
// assets, html_url, node_id, etc.). PublishedAt uses *time.Time so that
// JSON null on drafts decodes cleanly to nil rather than failing.
type rawRelease struct {
	ID              int64      `json:"id"`
	TagName         string     `json:"tag_name"`
	Name            string     `json:"name"`
	Draft           bool       `json:"draft"`
	Prerelease      bool       `json:"prerelease"`
	TargetCommitish string     `json:"target_commitish"`
	Body            string     `json:"body"`
	Author          *rawActor  `json:"author"`
	CreatedAt       time.Time  `json:"created_at"`
	PublishedAt     *time.Time `json:"published_at"`
}

type rawActor struct {
	Login string `json:"login"`
	ID    int64  `json:"id"`
	Type  string `json:"type"`
}

func (r rawRelease) toRelease() Release {
	var published time.Time
	if r.PublishedAt != nil {
		published = *r.PublishedAt
	}
	return Release{
		GHID:            r.ID,
		TagName:         r.TagName,
		Name:            r.Name,
		Draft:           r.Draft,
		Prerelease:      r.Prerelease,
		TargetCommitish: r.TargetCommitish,
		Body:            r.Body,
		Author:          parseActor(r.Author),
		CreatedAt:       r.CreatedAt,
		PublishedAt:     published,
	}
}

func parseActor(r *rawActor) prs.Author {
	if r == nil {
		return prs.Author{Type: "Ghost"}
	}
	return prs.Author{GHID: r.ID, Login: r.Login, Type: r.Type}
}
