//go:build gen

// This file is gated by the `gen` build tag so it only runs when explicitly
// invoked. It authors the testdata cassettes for the releases fetcher by
// writing `vcr.Cassette` JSON directly — no real network calls.
//
// Re-run after any change to the request shape or fixture content:
//
//	go test -tags=gen -run TestGen_Cassettes ./internal/github/releases/...
//
// The generated files are committed; CI replays them via the default
// (no-tag) build.

package releases

import (
	"encoding/json"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/karnstack/tempo/internal/github/vcr"
)

func TestGen_Cassettes(t *testing.T) {
	mustWrite := func(t *testing.T, path string, c *vcr.Cassette) {
		t.Helper()
		if err := c.Save(path); err != nil {
			t.Fatalf("save %s: %v", path, err)
		}
		t.Logf("wrote %s (%d interaction(s))", path, len(c.Interactions))
	}

	const (
		owner = "karnstack"
		repo  = "tempo"
	)

	t.Run("list_page", func(t *testing.T) {
		reqURL := releasesRequestURL(owner, repo, 1, 100)

		// Three releases exercising the variants we care about:
		//   1. v1.0.0 stable — User author, published, not draft/prerelease.
		//   2. v1.1.0-rc.1 — User author, published, prerelease=true.
		//   3. v2.0.0-draft — null author -> Ghost, draft=true,
		//      published_at=null -> zero PublishedAt.
		body := mustMarshal(t, []map[string]any{
			{
				"id":               int64(7001),
				"tag_name":         "v1.0.0",
				"name":             "v1.0.0 — first stable",
				"draft":            false,
				"prerelease":       false,
				"target_commitish": "main",
				"body":             "First stable release.",
				"author":           map[string]any{"login": "alice", "id": 2001, "type": "User"},
				"created_at":       "2026-04-12T10:00:00Z",
				"published_at":     "2026-04-12T10:05:00Z",
			},
			{
				"id":               int64(7002),
				"tag_name":         "v1.1.0-rc.1",
				"name":             "v1.1.0 release candidate 1",
				"draft":            false,
				"prerelease":       true,
				"target_commitish": "release/1.1",
				"body":             "RC for v1.1.0; do not deploy to production.",
				"author":           map[string]any{"login": "bob", "id": 2002, "type": "User"},
				"created_at":       "2026-04-11T08:00:00Z",
				"published_at":     "2026-04-11T08:30:00Z",
			},
			{
				"id":               int64(7003),
				"tag_name":         "v2.0.0-draft",
				"name":             "v2.0.0 (draft)",
				"draft":            true,
				"prerelease":       false,
				"target_commitish": "main",
				"body":             "Draft notes for v2 cut.",
				"author":           nil, // null -> Ghost
				"created_at":       "2026-04-10T12:30:00Z",
				"published_at":     nil, // null on drafts -> zero PublishedAt
			},
		})

		c := &vcr.Cassette{
			Version: vcr.CassetteVersion,
			Interactions: []vcr.Interaction{{
				Request: vcr.Request{
					Method: http.MethodGet,
					URL:    reqURL,
				},
				Response: vcr.Response{
					Status: http.StatusOK,
					Headers: http.Header{
						"Etag": []string{`W/"rel-abc"`},
						"Link": []string{
							`<https://api.github.com/repositories/1/releases?page=2&per_page=100>; rel="next", ` +
								`<https://api.github.com/repositories/1/releases?page=4&per_page=100>; rel="last"`,
						},
					},
					Body: body,
				},
			}},
		}
		mustWrite(t, filepath.Join("testdata", "list_page.json"), c)
	})

	t.Run("list_not_modified", func(t *testing.T) {
		reqURL := releasesRequestURL(owner, repo, 1, 100)

		c := &vcr.Cassette{
			Version: vcr.CassetteVersion,
			Interactions: []vcr.Interaction{{
				Request: vcr.Request{
					Method: http.MethodGet,
					URL:    reqURL,
				},
				Response: vcr.Response{
					Status: http.StatusNotModified,
					// Server typically omits ETag on 304; exercise the
					// fetcher's caller-echo fallback path.
				},
			}},
		}
		mustWrite(t, filepath.Join("testdata", "list_not_modified.json"), c)
	})

	t.Run("list_http_error", func(t *testing.T) {
		reqURL := releasesRequestURL("ghost-org", "missing-repo", 1, 100)
		body := mustMarshal(t, map[string]any{
			"message":           "Not Found",
			"documentation_url": "https://docs.github.com/rest",
		})

		c := &vcr.Cassette{
			Version: vcr.CassetteVersion,
			Interactions: []vcr.Interaction{{
				Request: vcr.Request{
					Method: http.MethodGet,
					URL:    reqURL,
				},
				Response: vcr.Response{
					Status: http.StatusNotFound,
					Body:   body,
				},
			}},
		}
		mustWrite(t, filepath.Join("testdata", "list_http_error.json"), c)
	})
}

// releasesRequestURL builds the canonical URL the releases fetcher would
// send for these arguments. Mirrors the path + query construction in
// fetcher.go so the cassette matches exactly.
func releasesRequestURL(owner, repo string, page, perPage int) string {
	q := url.Values{}
	q.Set("page", strconv.Itoa(page))
	q.Set("per_page", strconv.Itoa(perPage))
	return "https://api.github.com/repos/" + owner + "/" + repo + "/releases?" + q.Encode()
}

func mustMarshal(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}
