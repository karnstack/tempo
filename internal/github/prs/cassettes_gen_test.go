//go:build gen

// This file is gated by the `gen` build tag so it only runs when explicitly
// invoked. It authors the testdata cassettes for the prs fetcher by writing
// `vcr.Cassette` JSON directly — no real network calls.
//
// Re-run after any change to the listQuery string or fixture shape:
//
//	go test -tags=gen -run TestGen_Cassettes ./internal/github/prs/...
//
// The generated files are committed; CI replays them via the default
// (no-tag) build.

package prs

import (
	"encoding/json"
	"path/filepath"
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

	t.Run("list_page", func(t *testing.T) {
		req := graphQLRequestBody(t, "karnstack", "tempo", 100, nil)
		nodes := []map[string]any{
			prNode(map[string]any{
				"databaseId":  int64(1000001),
				"number":      101,
				"title":       "Add cycle-time chart",
				"state":       "MERGED",
				"createdAt":   "2026-04-10T09:00:00Z",
				"updatedAt":   "2026-04-12T15:30:00Z",
				"mergedAt":    "2026-04-12T15:30:00Z",
				"closedAt":    "2026-04-12T15:30:00Z",
				"additions":   120,
				"deletions":   45,
				"baseRefName": "main",
				"headRefName": "feat/cycle-time",
				"isDraft":     false,
				"author": map[string]any{
					"__typename": "User",
					"login":      "alice",
					"databaseId": int64(2001),
				},
			}),
			prNode(map[string]any{
				"databaseId":  int64(1000002),
				"number":      100,
				"title":       "chore(deps): bump zap to v1.27",
				"state":       "CLOSED",
				"createdAt":   "2026-04-08T12:00:00Z",
				"updatedAt":   "2026-04-11T08:15:00Z",
				"mergedAt":    nil,
				"closedAt":    "2026-04-11T08:15:00Z",
				"additions":   12,
				"deletions":   12,
				"baseRefName": "main",
				"headRefName": "renovate/zap-1.x",
				"isDraft":     false,
				"author": map[string]any{
					"__typename": "Bot",
					"login":      "renovate[bot]",
					"databaseId": int64(2002),
				},
			}),
			prNode(map[string]any{
				"databaseId":  int64(1000003),
				"number":      99,
				"title":       "WIP: sketch deploys ingest",
				"state":       "OPEN",
				"createdAt":   "2026-04-09T17:45:00Z",
				"updatedAt":   "2026-04-10T18:00:00Z",
				"mergedAt":    nil,
				"closedAt":    nil,
				"additions":   8,
				"deletions":   0,
				"baseRefName": "main",
				"headRefName": "wip/deploys",
				"isDraft":     true,
				"author": map[string]any{
					"__typename": "Mannequin",
					"login":      "old-bob",
					"databaseId": int64(2003),
				},
			}),
			prNode(map[string]any{
				"databaseId":  int64(1000004),
				"number":      98,
				"title":       "Tiny typo fix",
				"state":       "OPEN",
				"createdAt":   "2026-04-09T07:00:00Z",
				"updatedAt":   "2026-04-09T07:00:00Z",
				"mergedAt":    nil,
				"closedAt":    nil,
				"additions":   1,
				"deletions":   1,
				"baseRefName": "main",
				"headRefName": "fix/typo",
				"isDraft":     false,
				"author":      nil, // Ghost
			}),
		}
		resp := graphQLResponseBody(t, map[string]any{
			"hasNextPage": true,
			"endCursor":   "Y3Vyc29yOjQ=",
		}, nodes)

		c := &vcr.Cassette{
			Version: vcr.CassetteVersion,
			Interactions: []vcr.Interaction{{
				Request:  vcr.Request{Method: "POST", URL: "https://api.github.com/graphql", Body: req},
				Response: vcr.Response{Status: 200, Body: resp},
			}},
		}
		mustWrite(t, filepath.Join("testdata", "list_page.json"), c)
	})

	t.Run("list_since_cutoff", func(t *testing.T) {
		// since = 2026-04-01T00:00:00Z. Two PRs above, one below.
		req := graphQLRequestBody(t, "karnstack", "tempo", 100, nil)
		nodes := []map[string]any{
			prNode(map[string]any{
				"databaseId":  int64(2000001),
				"number":      55,
				"title":       "Recent thing",
				"state":       "OPEN",
				"createdAt":   "2026-04-05T00:00:00Z",
				"updatedAt":   "2026-04-05T00:00:00Z",
				"mergedAt":    nil,
				"closedAt":    nil,
				"additions":   3,
				"deletions":   1,
				"baseRefName": "main",
				"headRefName": "feat/recent",
				"isDraft":     false,
				"author": map[string]any{
					"__typename": "User",
					"login":      "alice",
					"databaseId": int64(2001),
				},
			}),
			prNode(map[string]any{
				"databaseId":  int64(2000002),
				"number":      54,
				"title":       "Just above cutoff",
				"state":       "MERGED",
				"createdAt":   "2026-03-25T00:00:00Z",
				"updatedAt":   "2026-04-01T00:00:01Z", // strictly after `since`
				"mergedAt":    "2026-04-01T00:00:01Z",
				"closedAt":    "2026-04-01T00:00:01Z",
				"additions":   10,
				"deletions":   2,
				"baseRefName": "main",
				"headRefName": "feat/above",
				"isDraft":     false,
				"author": map[string]any{
					"__typename": "User",
					"login":      "alice",
					"databaseId": int64(2001),
				},
			}),
			prNode(map[string]any{
				"databaseId":  int64(2000003),
				"number":      53,
				"title":       "Exactly on cutoff (should drop)",
				"state":       "MERGED",
				"createdAt":   "2026-03-20T00:00:00Z",
				"updatedAt":   "2026-04-01T00:00:00Z", // == since → dropped
				"mergedAt":    "2026-04-01T00:00:00Z",
				"closedAt":    "2026-04-01T00:00:00Z",
				"additions":   5,
				"deletions":   5,
				"baseRefName": "main",
				"headRefName": "feat/edge",
				"isDraft":     false,
				"author": map[string]any{
					"__typename": "User",
					"login":      "bob",
					"databaseId": int64(2010),
				},
			}),
		}
		resp := graphQLResponseBody(t, map[string]any{
			"hasNextPage": false,
			"endCursor":   "Y3Vyc29yOjM=",
		}, nodes)

		c := &vcr.Cassette{
			Version: vcr.CassetteVersion,
			Interactions: []vcr.Interaction{{
				Request:  vcr.Request{Method: "POST", URL: "https://api.github.com/graphql", Body: req},
				Response: vcr.Response{Status: 200, Body: resp},
			}},
		}
		mustWrite(t, filepath.Join("testdata", "list_since_cutoff.json"), c)
	})

	t.Run("list_graphql_error", func(t *testing.T) {
		req := graphQLRequestBody(t, "ghost-org", "missing-repo", 100, nil)
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
		mustWrite(t, filepath.Join("testdata", "list_graphql_error.json"), c)
	})
}

// graphQLRequestBody builds the canonical GraphQL request body the prs
// fetcher would send for these arguments. Mirrors the envelope shape in
// internal/github/client.go (`Client.GraphQL`).
func graphQLRequestBody(t *testing.T, owner, repo string, first int, after any) json.RawMessage {
	t.Helper()
	return mustMarshal(t, map[string]any{
		"query": listQuery,
		"variables": map[string]any{
			"owner": owner,
			"repo":  repo,
			"first": first,
			"after": after,
		},
	})
}

func graphQLResponseBody(t *testing.T, pageInfo map[string]any, nodes []map[string]any) json.RawMessage {
	t.Helper()
	return mustMarshal(t, map[string]any{
		"data": map[string]any{
			"repository": map[string]any{
				"pullRequests": map[string]any{
					"pageInfo": pageInfo,
					"nodes":    nodes,
				},
			},
		},
	})
}

func prNode(fields map[string]any) map[string]any { return fields }

func mustMarshal(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}

// Touch time so the import isn't unused if a fixture stops using time later.
var _ = time.Time{}
