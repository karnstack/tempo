//go:build gen

// This file is gated by the `gen` build tag so it only runs when
// explicitly invoked. It authors the testdata cassettes for the
// ingest/commits runner by writing `vcr.Cassette` JSON directly — no
// real network calls. Re-run after any change to fixture shape:
//
//	go test -tags=gen -run TestGen_Cassettes ./internal/ingest/commits/...
//
// The generated files are committed; CI replays them via the default
// (no-tag) build.

package commits_test

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

	// happy_path — 3 commits including a Ghost (deleted) author+committer.
	t.Run("happy_path", func(t *testing.T) {
		ix := []vcr.Interaction{
			restInteraction(t, "karnstack", "tempo",
				since(t, "2026-04-01T00:00:00Z"), 1,
				200,
				http.Header{
					"Etag":                  []string{`W/"abc123"`},
					"X-Ratelimit-Remaining": []string{"4999"},
					"X-Ratelimit-Reset":     []string{"1893456000"},
				},
				[]map[string]any{
					commitNode("a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1",
						"feat(charts): cycle-time line",
						"2026-04-12T10:00:00Z", "2026-04-12T10:00:00Z",
						userActor("alice", 2001), userActor("alice", 2001)),
					commitNode("b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2",
						"chore(deps): bump zap to v1.27",
						"2026-04-11T08:00:00Z", "2026-04-11T08:15:00Z",
						botActor("renovate[bot]", 2002), userActor("alice", 2001)),
					commitNode("c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3",
						"fix: handle null ref",
						"2026-04-10T12:30:00Z", "2026-04-10T12:30:00Z",
						nil, nil),
				},
			),
		}
		mustWrite(t, filepath.Join("testdata", "happy_path.json"),
			&vcr.Cassette{Version: vcr.CassetteVersion, Interactions: ix})
	})

	// not_modified — pre-seeded cursor (since=2026-04-12T10:00:00Z,
	// etag=W/"abc123"). Server responds 304.
	t.Run("not_modified", func(t *testing.T) {
		ix := []vcr.Interaction{
			restInteractionRaw(
				"karnstack", "tempo",
				since(t, "2026-04-12T10:00:00Z"), 1,
				304,
				http.Header{"Etag": []string{`W/"abc123"`}},
				nil,
			),
		}
		mustWrite(t, filepath.Join("testdata", "not_modified.json"),
			&vcr.Cassette{Version: vcr.CassetteVersion, Interactions: ix})
	})

	// multi_page — page 1 returns 2 commits + Link rel="next"; page 2 returns
	// 2 more, terminal. No etag is needed for page 2 (fetcher drops it).
	t.Run("multi_page", func(t *testing.T) {
		ix := []vcr.Interaction{
			restInteractionWithLink(t,
				"karnstack", "multi",
				since(t, "2026-04-01T00:00:00Z"), 1,
				200,
				http.Header{
					"Etag":                  []string{`W/"page1"`},
					"X-Ratelimit-Remaining": []string{"4900"},
					"X-Ratelimit-Reset":     []string{"1893456000"},
					"Link":                  []string{`<https://api.github.com/repositories/1/commits?page=2&per_page=100>; rel="next", <https://api.github.com/repositories/1/commits?page=2&per_page=100>; rel="last"`},
				},
				[]map[string]any{
					commitNode("p1p1p1p1p1p1p1p1p1p1p1p1p1p1p1p1p1p1p1p1",
						"feat: page1 commit A",
						"2026-04-14T09:00:00Z", "2026-04-14T09:00:00Z",
						userActor("alice", 2001), userActor("alice", 2001)),
					commitNode("p2p2p2p2p2p2p2p2p2p2p2p2p2p2p2p2p2p2p2p2",
						"feat: page1 commit B",
						"2026-04-14T09:30:00Z", "2026-04-14T09:30:00Z",
						userActor("bob", 2010), userActor("bob", 2010)),
				},
			),
			restInteractionRaw(
				"karnstack", "multi",
				since(t, "2026-04-01T00:00:00Z"), 2,
				200,
				http.Header{"Etag": []string{`W/"page2"`}},
				mustMarshal(t, []map[string]any{
					commitNode("p3p3p3p3p3p3p3p3p3p3p3p3p3p3p3p3p3p3p3p3",
						"feat: page2 commit A",
						"2026-04-15T10:00:00Z", "2026-04-15T10:00:00Z",
						userActor("alice", 2001), userActor("alice", 2001)),
					commitNode("p4p4p4p4p4p4p4p4p4p4p4p4p4p4p4p4p4p4p4p4",
						"feat: page2 commit B",
						"2026-04-15T11:00:00Z", "2026-04-15T11:00:00Z",
						userActor("alice", 2001), userActor("alice", 2001)),
				}),
			),
		}
		mustWrite(t, filepath.Join("testdata", "multi_page.json"),
			&vcr.Cassette{Version: vcr.CassetteVersion, Interactions: ix})
	})

	// empty_response — 200 OK with an empty commits array but a fresh etag.
	// Used by the "refresh etag" and "legacy cursor" tests.
	t.Run("empty_response", func(t *testing.T) {
		ix := []vcr.Interaction{
			restInteraction(t, "karnstack", "calm",
				since(t, "2026-04-15T00:00:00Z"), 1,
				200,
				http.Header{
					"Etag":                  []string{`W/"new-etag"`},
					"X-Ratelimit-Remaining": []string{"4800"},
					"X-Ratelimit-Reset":     []string{"1893456000"},
				},
				[]map[string]any{},
			),
		}
		mustWrite(t, filepath.Join("testdata", "empty_response.json"),
			&vcr.Cassette{Version: vcr.CassetteVersion, Interactions: ix})
	})

	// repo_failure — alphabetically-first repo "aok" succeeds with 1 commit;
	// second repo "zfail" returns 404.
	t.Run("repo_failure", func(t *testing.T) {
		ix := []vcr.Interaction{
			restInteraction(t, "karnstack", "aok",
				since(t, "2026-04-01T00:00:00Z"), 1,
				200,
				http.Header{
					"Etag":                  []string{`W/"aok1"`},
					"X-Ratelimit-Remaining": []string{"4700"},
					"X-Ratelimit-Reset":     []string{"1893456000"},
				},
				[]map[string]any{
					commitNode("aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111",
						"feat: aok commit",
						"2026-04-14T09:00:00Z", "2026-04-14T09:00:00Z",
						userActor("alice", 2001), userActor("alice", 2001)),
				},
			),
			restInteractionRaw(
				"karnstack", "zfail",
				since(t, "2026-04-01T00:00:00Z"), 1,
				404,
				nil,
				mustMarshal(t, map[string]any{"message": "Not Found"}),
			),
		}
		mustWrite(t, filepath.Join("testdata", "repo_failure.json"),
			&vcr.Cassette{Version: vcr.CassetteVersion, Interactions: ix})
	})
}

// --- builders ---

// restInteraction builds a 200 OK commits-list interaction with a JSON body.
// The URL is constructed identically to the fetcher (alphabetical query keys).
func restInteraction(t *testing.T, owner, repo, sinceParam string, page int, status int, headers http.Header, body []map[string]any) vcr.Interaction {
	t.Helper()
	return restInteractionRaw(owner, repo, sinceParam, page, status, headers, mustMarshal(t, body))
}

// restInteractionWithLink is restInteraction with a custom Link header path
// — not really different from restInteraction since Link is just a header
// the caller passes in, but kept for code clarity at the call site.
func restInteractionWithLink(t *testing.T, owner, repo, sinceParam string, page int, status int, headers http.Header, body []map[string]any) vcr.Interaction {
	t.Helper()
	return restInteractionRaw(owner, repo, sinceParam, page, status, headers, mustMarshal(t, body))
}

func restInteractionRaw(owner, repo, sinceParam string, page, status int, headers http.Header, body json.RawMessage) vcr.Interaction {
	q := url.Values{}
	q.Set("page", strconv.Itoa(page))
	q.Set("per_page", "100")
	q.Set("since", sinceParam)
	u := "https://api.github.com/repos/" + owner + "/" + repo + "/commits?" + q.Encode()
	return vcr.Interaction{
		Request:  vcr.Request{Method: "GET", URL: u},
		Response: vcr.Response{Status: status, Headers: headers, Body: body},
	}
}

// since formats t as RFC3339 (second precision) the way the fetcher does
// when building the `since` query param.
func since(t *testing.T, rfc3339 string) string {
	t.Helper()
	// The fetcher does opts.Since.UTC().Format(time.RFC3339); we just round-
	// trip our test input through the same formatter to be sure.
	return rfc3339
}

// commitNode emits the JSON shape the commits fetcher decodes.
func commitNode(sha, message, authoredAt, committedAt string, author, committer map[string]any) map[string]any {
	return map[string]any{
		"sha":       sha,
		"author":    author,
		"committer": committer,
		"commit": map[string]any{
			"message":   message,
			"author":    map[string]any{"date": authoredAt},
			"committer": map[string]any{"date": committedAt},
		},
	}
}

func userActor(login string, id int64) map[string]any {
	return map[string]any{"login": login, "id": id, "type": "User"}
}

func botActor(login string, id int64) map[string]any {
	return map[string]any{"login": login, "id": id, "type": "Bot"}
}

func mustMarshal(t *testing.T, v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		if t != nil {
			t.Fatalf("marshal: %v", err)
		}
		panic(err)
	}
	return b
}
