//go:build gen

// This file is gated by the `gen` build tag so it only runs when
// explicitly invoked. It authors the testdata cassettes for the
// orgrepos fetcher by writing `vcr.Cassette` JSON directly — no real
// network calls.
//
// Re-run after any change to the request shape or fixture content:
//
//	go test -tags=gen -run TestGen_Cassettes ./internal/github/orgrepos/...
//
// The generated files are committed; CI replays them via the default
// (no-tag) build.

package orgrepos

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

	const org = "karnstack"

	t.Run("list_page", func(t *testing.T) {
		reqURL := orgreposRequestURL(org, 1, 100, "")

		// Three repos exercising the variants the ingest worker cares
		// about:
		//   1. tempo — regular public repo on `main`.
		//   2. legacy-archive — archived=true, default_branch=master.
		//   3. forked-toy — fork=true.
		body := mustMarshal(t, []map[string]any{
			{
				"id":             int64(9001),
				"name":           "tempo",
				"default_branch": "main",
				"archived":       false,
				"fork":           false,
				"private":        false,
				"owner": map[string]any{
					"login": "karnstack",
					"id":    1001,
					"type":  "Organization",
				},
			},
			{
				"id":             int64(9002),
				"name":           "legacy-archive",
				"default_branch": "master",
				"archived":       true,
				"fork":           false,
				"private":        false,
				"owner": map[string]any{
					"login": "karnstack",
					"id":    1001,
					"type":  "Organization",
				},
			},
			{
				"id":             int64(9003),
				"name":           "forked-toy",
				"default_branch": "main",
				"archived":       false,
				"fork":           true,
				"private":        false,
				"owner": map[string]any{
					"login": "karnstack",
					"id":    1001,
					"type":  "Organization",
				},
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
						"Etag": []string{`W/"orgrepos-abc"`},
						"Link": []string{
							`<https://api.github.com/organizations/1001/repos?page=2&per_page=100>; rel="next", ` +
								`<https://api.github.com/organizations/1001/repos?page=4&per_page=100>; rel="last"`,
						},
					},
					Body: body,
				},
			}},
		}
		mustWrite(t, filepath.Join("testdata", "list_page.json"), c)
	})

	t.Run("list_not_modified", func(t *testing.T) {
		reqURL := orgreposRequestURL(org, 1, 100, "")

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
		reqURL := orgreposRequestURL("ghost-org", 1, 100, "")
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

// orgreposRequestURL builds the canonical URL the orgrepos fetcher
// would send for these arguments. Mirrors the path + query construction
// in fetcher.go so the cassette matches exactly.
func orgreposRequestURL(org string, page, perPage int, typ string) string {
	q := url.Values{}
	q.Set("page", strconv.Itoa(page))
	q.Set("per_page", strconv.Itoa(perPage))
	if typ != "" {
		q.Set("type", typ)
	}
	return "https://api.github.com/orgs/" + org + "/repos?" + q.Encode()
}

func mustMarshal(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}
