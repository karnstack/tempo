//go:build gen

// This file is gated by the `gen` build tag so it only runs when explicitly
// invoked. It authors the testdata cassettes for the deployments fetcher
// by writing `vcr.Cassette` JSON directly — no real network calls.
//
// Re-run after any change to the request shape or fixture content:
//
//	go test -tags=gen -run TestGen_Cassettes ./internal/github/deployments/...
//
// The generated files are committed; CI replays them via the default
// (no-tag) build.

package deployments

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
		env   = "production"
	)

	t.Run("list_page", func(t *testing.T) {
		reqURL := deploymentsRequestURL(owner, repo, 1, 100, "", "", env)

		// Three deployments exercising the creator variants we care about:
		//   1. User creator (alice deploys to production).
		//   2. Bot creator (deploybot pushes a hotfix tag).
		//   3. Null creator -> Ghost.
		body := mustMarshal(t, []map[string]any{
			{
				"id":          int64(5001),
				"sha":         "a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1",
				"ref":         "main",
				"task":        "deploy",
				"environment": "production",
				"description": "Auto-deploy from main",
				"creator":     map[string]any{"login": "alice", "id": 2001, "type": "User"},
				"created_at":  "2026-04-12T10:00:00Z",
				"updated_at":  "2026-04-12T10:00:00Z",
			},
			{
				"id":          int64(5002),
				"sha":         "b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2",
				"ref":         "hotfix/2026-04-11",
				"task":        "deploy:hotfix",
				"environment": "production",
				"description": "Deploybot rolled out v1.7.2",
				"creator":     map[string]any{"login": "deploybot", "id": 3001, "type": "Bot"},
				"created_at":  "2026-04-11T08:00:00Z",
				"updated_at":  "2026-04-11T08:05:00Z",
			},
			{
				"id":          int64(5003),
				"sha":         "c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3",
				"ref":         "main",
				"task":        "deploy",
				"environment": "production",
				"description": nil, // exercise null -> "" path
				"creator":     nil, // exercise null -> Ghost
				"created_at":  "2026-04-10T12:30:00Z",
				"updated_at":  "2026-04-10T12:30:00Z",
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
						"Etag": []string{`W/"dep-abc"`},
						"Link": []string{
							`<https://api.github.com/repositories/1/deployments?page=2&per_page=100&environment=production>; rel="next", ` +
								`<https://api.github.com/repositories/1/deployments?page=4&per_page=100&environment=production>; rel="last"`,
						},
					},
					Body: body,
				},
			}},
		}
		mustWrite(t, filepath.Join("testdata", "list_page.json"), c)
	})

	t.Run("list_not_modified", func(t *testing.T) {
		reqURL := deploymentsRequestURL(owner, repo, 1, 100, "", "", env)

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
		reqURL := deploymentsRequestURL("ghost-org", "missing-repo", 1, 100, "", "", "")
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

// deploymentsRequestURL builds the canonical URL the deployments fetcher
// would send for these arguments. Mirrors the path + query construction
// in fetcher.go so the cassette matches exactly.
func deploymentsRequestURL(owner, repo string, page, perPage int, sha, ref, env string) string {
	q := url.Values{}
	q.Set("page", strconv.Itoa(page))
	q.Set("per_page", strconv.Itoa(perPage))
	if sha != "" {
		q.Set("sha", sha)
	}
	if ref != "" {
		q.Set("ref", ref)
	}
	if env != "" {
		q.Set("environment", env)
	}
	return "https://api.github.com/repos/" + owner + "/" + repo + "/deployments?" + q.Encode()
}

func mustMarshal(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}
