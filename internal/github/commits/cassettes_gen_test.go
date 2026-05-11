//go:build gen

// This file is gated by the `gen` build tag so it only runs when explicitly
// invoked. It authors the testdata cassettes for the commits fetcher by
// writing `vcr.Cassette` JSON directly — no real network calls.
//
// Re-run after any change to the request shape or fixture content:
//
//	go test -tags=gen -run TestGen_Cassettes ./internal/github/commits/...
//
// The generated files are committed; CI replays them via the default
// (no-tag) build.

package commits

import (
	"encoding/json"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"testing"
	"time"

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
		// since chosen so the recorded request URL is stable across machines.
		since = "2026-03-01T00:00:00Z"
	)

	t.Run("list_page", func(t *testing.T) {
		reqURL := commitsRequestURL(owner, repo, 1, 100, since, "")

		// Three commits exercising the actor variants we care about:
		//   1. User author + User committer (alice authored, alice committed).
		//   2. Bot author (renovate[bot]) + User committer (alice) — committedAt
		//      strictly after authoredAt to prove the two timestamps are
		//      independent in the type.
		//   3. Null author + null committer -> Ghost on both ends.
		body := mustMarshal(t, []map[string]any{
			{
				"sha": "a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1",
				"commit": map[string]any{
					"message": "feat(charts): cycle-time line",
					"author": map[string]any{
						"name":  "Alice",
						"email": "alice@example.test",
						"date":  "2026-04-12T10:00:00Z",
					},
					"committer": map[string]any{
						"name":  "Alice",
						"email": "alice@example.test",
						"date":  "2026-04-12T10:00:00Z",
					},
				},
				"author":    map[string]any{"login": "alice", "id": 2001, "type": "User"},
				"committer": map[string]any{"login": "alice", "id": 2001, "type": "User"},
			},
			{
				"sha": "b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2",
				"commit": map[string]any{
					"message": "chore(deps): bump zap to v1.27",
					"author": map[string]any{
						"name":  "renovate[bot]",
						"email": "bot@renovate.test",
						"date":  "2026-04-11T08:00:00Z",
					},
					"committer": map[string]any{
						"name":  "Alice",
						"email": "alice@example.test",
						"date":  "2026-04-11T08:15:00Z",
					},
				},
				"author":    map[string]any{"login": "renovate[bot]", "id": 2002, "type": "Bot"},
				"committer": map[string]any{"login": "alice", "id": 2001, "type": "User"},
			},
			{
				"sha": "c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3",
				"commit": map[string]any{
					"message": "fix: handle null ref",
					"author": map[string]any{
						"name":  "Removed User",
						"email": "removed@example.test",
						"date":  "2026-04-10T12:30:00Z",
					},
					"committer": map[string]any{
						"name":  "Removed User",
						"email": "removed@example.test",
						"date":  "2026-04-10T12:30:00Z",
					},
				},
				"author":    nil,
				"committer": nil,
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
						"Etag": []string{`W/"abc123"`},
						"Link": []string{
							`<https://api.github.com/repositories/1/commits?page=2&per_page=100>; rel="next", ` +
								`<https://api.github.com/repositories/1/commits?page=4&per_page=100>; rel="last"`,
						},
					},
					Body: body,
				},
			}},
		}
		mustWrite(t, filepath.Join("testdata", "list_page.json"), c)
	})

	t.Run("list_not_modified", func(t *testing.T) {
		// Same request shape as list_page (caller polls with same since); the
		// recorded If-None-Match isn't part of the cassette match key, so we
		// don't need to embed it in the request.
		reqURL := commitsRequestURL(owner, repo, 1, 100, since, "")

		c := &vcr.Cassette{
			Version: vcr.CassetteVersion,
			Interactions: []vcr.Interaction{{
				Request: vcr.Request{
					Method: http.MethodGet,
					URL:    reqURL,
				},
				Response: vcr.Response{
					Status: http.StatusNotModified,
					// Server typically omits ETag on 304 in practice; we
					// exercise that path so the fetcher's caller-echo fallback
					// is covered.
				},
			}},
		}
		mustWrite(t, filepath.Join("testdata", "list_not_modified.json"), c)
	})

	t.Run("list_http_error", func(t *testing.T) {
		reqURL := commitsRequestURL("ghost-org", "missing-repo", 1, 100, "", "")
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

// commitsRequestURL builds the canonical URL the commits fetcher would
// send for these arguments. Mirrors the path + query construction in
// fetcher.go so the cassette matches exactly.
func commitsRequestURL(owner, repo string, page, perPage int, since, sha string) string {
	q := url.Values{}
	q.Set("page", strconv.Itoa(page))
	q.Set("per_page", strconv.Itoa(perPage))
	if since != "" {
		// since is already RFC3339 in the caller; format here for parity
		// with fetcher.go (which calls time.Time.Format).
		if t, err := time.Parse(time.RFC3339, since); err == nil {
			q.Set("since", t.UTC().Format(time.RFC3339))
		}
	}
	if sha != "" {
		q.Set("sha", sha)
	}
	return "https://api.github.com/repos/" + owner + "/" + repo + "/commits?" + q.Encode()
}

func mustMarshal(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}
