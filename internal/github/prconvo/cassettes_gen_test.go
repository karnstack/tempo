//go:build gen

// This file is gated by the `gen` build tag so it only runs when
// explicitly invoked. It authors the testdata cassettes for the
// prconvo fetcher by writing `vcr.Cassette` JSON directly — no real
// network calls.
//
// Re-run after any change to the GraphQL query strings or fixture
// shape:
//
//	go test -tags=gen -run TestGen_Cassettes ./internal/github/prconvo/...
//
// The generated files are committed; CI replays them via the default
// (no-tag) build.

package prconvo

import (
	"encoding/json"
	"path/filepath"
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

	t.Run("reviews_page", func(t *testing.T) {
		req := graphQLRequestBody(t, reviewsQuery, vars("karnstack", "tempo", 101, 100, nil))
		nodes := []map[string]any{
			reviewNode(map[string]any{
				"databaseId":  int64(3000001),
				"state":       "APPROVED",
				"submittedAt": "2026-04-11T12:00:00Z",
				"author": map[string]any{
					"__typename": "User",
					"login":      "alice",
					"databaseId": int64(2001),
				},
			}),
			reviewNode(map[string]any{
				"databaseId":  int64(3000002),
				"state":       "CHANGES_REQUESTED",
				"submittedAt": "2026-04-11T08:30:00Z",
				"author": map[string]any{
					"__typename": "User",
					"login":      "bob",
					"databaseId": int64(2010),
				},
			}),
			reviewNode(map[string]any{
				"databaseId":  int64(3000003),
				"state":       "COMMENTED",
				"submittedAt": "2026-04-10T17:45:00Z",
				"author": map[string]any{
					"__typename": "Bot",
					"login":      "review-bot",
					"databaseId": int64(2099),
				},
			}),
			reviewNode(map[string]any{
				"databaseId":  int64(3000004),
				"state":       "DISMISSED",
				"submittedAt": "2026-04-10T15:00:00Z",
				"author":      nil, // Ghost
			}),
		}
		resp := mustMarshal(t, map[string]any{
			"data": map[string]any{
				"repository": map[string]any{
					"pullRequest": map[string]any{
						"reviews": map[string]any{
							"pageInfo": map[string]any{
								"hasNextPage": true,
								"endCursor":   "Y3Vyc29yOnJ2LTQ=",
							},
							"nodes": nodes,
						},
					},
				},
			},
		})
		c := &vcr.Cassette{
			Version: vcr.CassetteVersion,
			Interactions: []vcr.Interaction{{
				Request:  vcr.Request{Method: "POST", URL: "https://api.github.com/graphql", Body: req},
				Response: vcr.Response{Status: 200, Body: resp},
			}},
		}
		mustWrite(t, filepath.Join("testdata", "reviews_page.json"), c)
	})

	t.Run("review_comments_page", func(t *testing.T) {
		req := graphQLRequestBody(t, reviewCommentsQuery, vars("karnstack", "tempo", 101, 100, nil))
		// Thread 1: two comments, no overflow.
		// Thread 2: one comment, BUT pageInfo.hasNextPage=true → Truncated.
		threads := []map[string]any{
			{
				"comments": map[string]any{
					"pageInfo": map[string]any{"hasNextPage": false},
					"nodes": []map[string]any{
						{
							"databaseId": int64(4000001),
							"createdAt":  "2026-04-11T09:15:00Z",
							"author": map[string]any{
								"__typename": "User",
								"login":      "alice",
								"databaseId": int64(2001),
							},
						},
						{
							"databaseId": int64(4000002),
							"createdAt":  "2026-04-11T09:20:00Z",
							"author": map[string]any{
								"__typename": "User",
								"login":      "carol",
								"databaseId": int64(2020),
							},
						},
					},
				},
			},
			{
				"comments": map[string]any{
					"pageInfo": map[string]any{"hasNextPage": true},
					"nodes": []map[string]any{
						{
							"databaseId": int64(4000003),
							"createdAt":  "2026-04-11T11:00:00Z",
							"author":     nil, // Ghost
						},
					},
				},
			},
		}
		resp := mustMarshal(t, map[string]any{
			"data": map[string]any{
				"repository": map[string]any{
					"pullRequest": map[string]any{
						"reviewThreads": map[string]any{
							"pageInfo": map[string]any{
								"hasNextPage": false,
								"endCursor":   "Y3Vyc29yOnRoLTI=",
							},
							"nodes": threads,
						},
					},
				},
			},
		})
		c := &vcr.Cassette{
			Version: vcr.CassetteVersion,
			Interactions: []vcr.Interaction{{
				Request:  vcr.Request{Method: "POST", URL: "https://api.github.com/graphql", Body: req},
				Response: vcr.Response{Status: 200, Body: resp},
			}},
		}
		mustWrite(t, filepath.Join("testdata", "review_comments_page.json"), c)
	})

	t.Run("issue_comments_page", func(t *testing.T) {
		req := graphQLRequestBody(t, issueCommentsQuery, vars("karnstack", "tempo", 101, 100, nil))
		nodes := []map[string]any{
			{
				"databaseId": int64(5000001),
				"createdAt":  "2026-04-11T10:00:00Z",
				"author": map[string]any{
					"__typename": "User",
					"login":      "alice",
					"databaseId": int64(2001),
				},
			},
			{
				"databaseId": int64(5000002),
				"createdAt":  "2026-04-11T10:05:00Z",
				"author": map[string]any{
					"__typename": "Bot",
					"login":      "ci-bot",
					"databaseId": int64(2030),
				},
			},
			{
				"databaseId": int64(5000003),
				"createdAt":  "2026-04-11T10:10:00Z",
				"author": map[string]any{
					"__typename": "Mannequin",
					"login":      "old-dan",
					"databaseId": int64(2040),
				},
			},
			{
				"databaseId": int64(5000004),
				"createdAt":  "2026-04-11T10:15:00Z",
				"author":     nil, // Ghost
			},
		}
		resp := mustMarshal(t, map[string]any{
			"data": map[string]any{
				"repository": map[string]any{
					"pullRequest": map[string]any{
						"comments": map[string]any{
							"pageInfo": map[string]any{
								"hasNextPage": false,
								"endCursor":   "Y3Vyc29yOmljLTQ=",
							},
							"nodes": nodes,
						},
					},
				},
			},
		})
		c := &vcr.Cassette{
			Version: vcr.CassetteVersion,
			Interactions: []vcr.Interaction{{
				Request:  vcr.Request{Method: "POST", URL: "https://api.github.com/graphql", Body: req},
				Response: vcr.Response{Status: 200, Body: resp},
			}},
		}
		mustWrite(t, filepath.Join("testdata", "issue_comments_page.json"), c)
	})

	t.Run("reviews_graphql_error", func(t *testing.T) {
		req := graphQLRequestBody(t, reviewsQuery, vars("ghost-org", "missing-repo", 1, 100, nil))
		body := mustMarshal(t, map[string]any{
			"data": nil,
			"errors": []map[string]any{{
				"message": "Could not resolve to a Repository with the name 'ghost-org/missing-repo'.",
				"type":    "NOT_FOUND",
				"path":    []any{"repository"},
			}},
		})
		c := &vcr.Cassette{
			Version: vcr.CassetteVersion,
			Interactions: []vcr.Interaction{{
				Request:  vcr.Request{Method: "POST", URL: "https://api.github.com/graphql", Body: req},
				Response: vcr.Response{Status: 200, Body: body},
			}},
		}
		mustWrite(t, filepath.Join("testdata", "reviews_graphql_error.json"), c)
	})
}

// graphQLRequestBody builds the canonical GraphQL request body the
// prconvo fetchers would send. Mirrors the envelope shape in
// internal/github/client.go (`Client.GraphQL`).
func graphQLRequestBody(t *testing.T, query string, variables map[string]any) json.RawMessage {
	t.Helper()
	return mustMarshal(t, map[string]any{
		"query":     query,
		"variables": variables,
	})
}

func vars(owner, repo string, number, first int, after any) map[string]any {
	return map[string]any{
		"owner":  owner,
		"repo":   repo,
		"number": number,
		"first":  first,
		"after":  after,
	}
}

func reviewNode(fields map[string]any) map[string]any { return fields }

func mustMarshal(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}
