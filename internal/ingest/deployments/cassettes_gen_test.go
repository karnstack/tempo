//go:build gen

// This file is gated by the `gen` build tag so it only runs when
// explicitly invoked. It authors the testdata cassettes for the
// ingest/deployments runner by writing `vcr.Cassette` JSON directly — no
// real network calls. Re-run after any change to fixture shape:
//
//	go test -tags=gen -run TestGen_Cassettes ./internal/ingest/deployments/...
//
// The generated files are committed; CI replays them via the default
// (no-tag) build.

package deployments_test

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

	// happy_path — 3 deploys including a Ghost (null) creator. Single
	// page, terminal (no Link rel="next").
	t.Run("happy_path", func(t *testing.T) {
		ix := []vcr.Interaction{
			depInteraction(t, "karnstack", "tempo", 1,
				200,
				http.Header{
					"Etag":                  []string{`W/"dep-abc"`},
					"X-Ratelimit-Remaining": []string{"4999"},
					"X-Ratelimit-Reset":     []string{"1893456000"},
				},
				[]map[string]any{
					deploymentNode(5001, "a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1",
						"main", "deploy", "production", "Auto-deploy from main",
						"2026-04-12T10:00:00Z", "2026-04-12T10:00:00Z",
						userActor("alice", 2001)),
					deploymentNode(5002, "b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2",
						"hotfix/2026-04-11", "deploy:hotfix", "production",
						"Deploybot rolled out v1.7.2",
						"2026-04-11T08:00:00Z", "2026-04-11T08:05:00Z",
						botActor("deploybot", 3001)),
					deploymentNode(5003, "c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3",
						"main", "deploy", "production", "",
						"2026-04-10T12:30:00Z", "2026-04-10T12:30:00Z",
						nil),
				},
			),
		}
		mustWrite(t, filepath.Join("testdata", "happy_path.json"),
			&vcr.Cassette{Version: vcr.CassetteVersion, Interactions: ix})
	})

	// not_modified — pre-seeded cursor (since=2026-04-12T10:00:00Z,
	// etag=W/"dep-abc"). Server responds 304.
	t.Run("not_modified", func(t *testing.T) {
		ix := []vcr.Interaction{
			depInteractionRaw("karnstack", "tempo", 1,
				304,
				http.Header{"Etag": []string{`W/"dep-abc"`}},
				nil,
			),
		}
		mustWrite(t, filepath.Join("testdata", "not_modified.json"),
			&vcr.Cassette{Version: vcr.CassetteVersion, Interactions: ix})
	})

	// multi_page — page 1 returns 2 deploys + Link rel="next"; page 2
	// returns 2 more, terminal. All deploys are newer than BackfillFrom
	// (2026-04-01), so no early-stop kicks in.
	t.Run("multi_page", func(t *testing.T) {
		ix := []vcr.Interaction{
			depInteraction(t, "karnstack", "multi", 1,
				200,
				http.Header{
					"Etag":                  []string{`W/"page1"`},
					"X-Ratelimit-Remaining": []string{"4900"},
					"X-Ratelimit-Reset":     []string{"1893456000"},
					"Link":                  []string{`<https://api.github.com/repositories/1/deployments?page=2&per_page=100>; rel="next", <https://api.github.com/repositories/1/deployments?page=2&per_page=100>; rel="last"`},
				},
				[]map[string]any{
					deploymentNode(6001, "p1p1p1p1p1p1p1p1p1p1p1p1p1p1p1p1p1p1p1p1",
						"main", "deploy", "production", "page1 A",
						"2026-04-14T09:30:00Z", "2026-04-14T09:30:00Z",
						userActor("alice", 2001)),
					deploymentNode(6002, "p2p2p2p2p2p2p2p2p2p2p2p2p2p2p2p2p2p2p2p2",
						"main", "deploy", "production", "page1 B",
						"2026-04-14T09:00:00Z", "2026-04-14T09:00:00Z",
						userActor("bob", 2010)),
				},
			),
			depInteractionRaw("karnstack", "multi", 2,
				200,
				http.Header{"Etag": []string{`W/"page2"`}},
				mustMarshal(t, []map[string]any{
					deploymentNode(6003, "p3p3p3p3p3p3p3p3p3p3p3p3p3p3p3p3p3p3p3p3",
						"main", "deploy", "production", "page2 A",
						"2026-04-09T10:00:00Z", "2026-04-09T10:00:00Z",
						userActor("alice", 2001)),
					deploymentNode(6004, "p4p4p4p4p4p4p4p4p4p4p4p4p4p4p4p4p4p4p4p4",
						"main", "deploy", "production", "page2 B",
						"2026-04-08T11:00:00Z", "2026-04-08T11:00:00Z",
						userActor("alice", 2001)),
				}),
			),
		}
		mustWrite(t, filepath.Join("testdata", "multi_page.json"),
			&vcr.Cassette{Version: vcr.CassetteVersion, Interactions: ix})
	})

	// early_stop — page 1 advertises rel="next" but contains 2 new + 1
	// old (relative to a seeded cursor of 2026-04-15). Runner must
	// process the 2 new ones and BREAK before fetching page 2. The
	// cassette intentionally has no page-2 interaction; vcr.Done() will
	// fail loud if the runner tries.
	t.Run("early_stop", func(t *testing.T) {
		ix := []vcr.Interaction{
			depInteraction(t, "karnstack", "tempo", 1,
				200,
				http.Header{
					"Etag":                  []string{`W/"dep-stop"`},
					"X-Ratelimit-Remaining": []string{"4800"},
					"X-Ratelimit-Reset":     []string{"1893456000"},
					"Link":                  []string{`<https://api.github.com/repositories/1/deployments?page=2&per_page=100>; rel="next", <https://api.github.com/repositories/1/deployments?page=2&per_page=100>; rel="last"`},
				},
				[]map[string]any{
					deploymentNode(7001, "n1n1n1n1n1n1n1n1n1n1n1n1n1n1n1n1n1n1n1n1",
						"main", "deploy", "production", "newest",
						"2026-04-20T10:00:00Z", "2026-04-20T10:00:00Z",
						userActor("alice", 2001)),
					deploymentNode(7002, "n2n2n2n2n2n2n2n2n2n2n2n2n2n2n2n2n2n2n2n2",
						"main", "deploy", "production", "middle",
						"2026-04-18T10:00:00Z", "2026-04-18T10:00:00Z",
						userActor("alice", 2001)),
					deploymentNode(7003, "o1o1o1o1o1o1o1o1o1o1o1o1o1o1o1o1o1o1o1o1",
						"main", "deploy", "production", "older",
						"2026-04-10T10:00:00Z", "2026-04-10T10:00:00Z",
						userActor("alice", 2001)),
				},
			),
		}
		mustWrite(t, filepath.Join("testdata", "early_stop.json"),
			&vcr.Cassette{Version: vcr.CassetteVersion, Interactions: ix})
	})

	// empty_response — 200 OK with an empty deploys array but a fresh
	// etag. Used by the "refresh etag" and "legacy cursor" tests.
	t.Run("empty_response", func(t *testing.T) {
		ix := []vcr.Interaction{
			depInteraction(t, "karnstack", "calm", 1,
				200,
				http.Header{
					"Etag":                  []string{`W/"dep-new"`},
					"X-Ratelimit-Remaining": []string{"4700"},
					"X-Ratelimit-Reset":     []string{"1893456000"},
				},
				[]map[string]any{},
			),
		}
		mustWrite(t, filepath.Join("testdata", "empty_response.json"),
			&vcr.Cassette{Version: vcr.CassetteVersion, Interactions: ix})
	})

	// repo_failure — alphabetically-first repo "aok" succeeds with 1
	// deploy; second repo "zfail" returns 404.
	t.Run("repo_failure", func(t *testing.T) {
		ix := []vcr.Interaction{
			depInteraction(t, "karnstack", "aok", 1,
				200,
				http.Header{
					"Etag":                  []string{`W/"aok1"`},
					"X-Ratelimit-Remaining": []string{"4600"},
					"X-Ratelimit-Reset":     []string{"1893456000"},
				},
				[]map[string]any{
					deploymentNode(8001, "aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111",
						"main", "deploy", "production", "aok deploy",
						"2026-04-14T09:00:00Z", "2026-04-14T09:00:00Z",
						userActor("alice", 2001)),
				},
			),
			depInteractionRaw("karnstack", "zfail", 1,
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

// depInteraction builds a deployments-list interaction with a JSON body.
// The URL is constructed identically to the fetcher (alphabetical query
// keys, no since param — the deployments endpoint doesn't accept one).
func depInteraction(t *testing.T, owner, repo string, page int, status int, headers http.Header, body []map[string]any) vcr.Interaction {
	t.Helper()
	return depInteractionRaw(owner, repo, page, status, headers, mustMarshal(t, body))
}

func depInteractionRaw(owner, repo string, page, status int, headers http.Header, body json.RawMessage) vcr.Interaction {
	q := url.Values{}
	q.Set("page", strconv.Itoa(page))
	q.Set("per_page", "100")
	u := "https://api.github.com/repos/" + owner + "/" + repo + "/deployments?" + q.Encode()
	return vcr.Interaction{
		Request:  vcr.Request{Method: "GET", URL: u},
		Response: vcr.Response{Status: status, Headers: headers, Body: body},
	}
}

// deploymentNode emits the JSON shape the deployments fetcher decodes.
func deploymentNode(id int64, sha, ref, task, env, description, createdAt, updatedAt string, creator map[string]any) map[string]any {
	return map[string]any{
		"id":          id,
		"sha":         sha,
		"ref":         ref,
		"task":        task,
		"environment": env,
		"description": description,
		"creator":     creator,
		"created_at":  createdAt,
		"updated_at":  updatedAt,
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
