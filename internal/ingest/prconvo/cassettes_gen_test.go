//go:build gen

// This file is gated by the `gen` build tag so it only runs when
// explicitly invoked. It authors the testdata cassettes for the
// ingest/prconvo runner by writing `vcr.Cassette` JSON directly — no
// real network calls. Re-run after any change to fixture shape:
//
//	go test -tags=gen -run TestGen_Cassettes ./internal/ingest/prconvo/...
//
// The generated files are committed; CI replays them via the default
// (no-tag) build.

package prconvo_test

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/karnstack/tempo/internal/github/prconvo"
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

	// happy_path_single_pr — one PR with 2 reviews (1 user-authored,
	// 1 ghost-dismissed), 2 review comments (1 user, 1 ghost), 1 issue
	// comment (bot-authored). No pagination for any sub-resource.
	t.Run("happy_path_single_pr", func(t *testing.T) {
		ix := []vcr.Interaction{
			gqlInteraction(t,
				prconvo.ReviewsQuery,
				vars("karnstack", "tempo", 101, 100, nil),
				reviewsResp(map[string]any{
					"pageInfo": pageInfo(false, ""),
					"nodes": []map[string]any{
						reviewNode(int64(3000001), "APPROVED", "2026-04-11T12:00:00Z",
							userAuthor("alice", 2001)),
						reviewNode(int64(3000002), "DISMISSED", "2026-04-11T13:00:00Z",
							nil), // ghost
					},
				}),
			),
			gqlInteraction(t,
				prconvo.ReviewCommentsQuery,
				vars("karnstack", "tempo", 101, 100, nil),
				reviewCommentsResp(map[string]any{
					"pageInfo": pageInfo(false, ""),
					"nodes": []map[string]any{
						{
							"comments": map[string]any{
								"pageInfo": map[string]any{"hasNextPage": false},
								"nodes": []map[string]any{
									reviewCommentNode(int64(4000001), "2026-04-11T12:15:00Z",
										userAuthor("alice", 2001)),
									reviewCommentNode(int64(4000002), "2026-04-11T12:20:00Z",
										nil), // ghost
								},
							},
						},
					},
				}),
			),
			gqlInteraction(t,
				prconvo.IssueCommentsQuery,
				vars("karnstack", "tempo", 101, 100, nil),
				issueCommentsResp(map[string]any{
					"pageInfo": pageInfo(false, ""),
					"nodes": []map[string]any{
						issueCommentNode(int64(5000001), "2026-04-11T11:00:00Z",
							botAuthor("ci-bot", 2030)),
					},
				}),
			),
		}
		mustWrite(t, filepath.Join("testdata", "happy_path_single_pr.json"),
			&vcr.Cassette{Version: vcr.CassetteVersion, Interactions: ix})
	})

	// two_prs — two PRs, each with one review, one review comment, one
	// issue comment. Establishes that the runner walks PRs in ASC
	// updated_at order and advances cursor to the max.
	t.Run("two_prs", func(t *testing.T) {
		ix := []vcr.Interaction{
			// PR #101 (older updated_at) first.
			gqlInteraction(t, prconvo.ReviewsQuery,
				vars("karnstack", "multi", 101, 100, nil),
				reviewsResp(map[string]any{
					"pageInfo": pageInfo(false, ""),
					"nodes": []map[string]any{
						reviewNode(int64(3100001), "APPROVED", "2026-04-12T10:00:00Z",
							userAuthor("alice", 2001)),
					},
				}),
			),
			gqlInteraction(t, prconvo.ReviewCommentsQuery,
				vars("karnstack", "multi", 101, 100, nil),
				reviewCommentsResp(map[string]any{
					"pageInfo": pageInfo(false, ""),
					"nodes": []map[string]any{
						{
							"comments": map[string]any{
								"pageInfo": map[string]any{"hasNextPage": false},
								"nodes": []map[string]any{
									reviewCommentNode(int64(4100001), "2026-04-12T10:30:00Z",
										userAuthor("alice", 2001)),
								},
							},
						},
					},
				}),
			),
			gqlInteraction(t, prconvo.IssueCommentsQuery,
				vars("karnstack", "multi", 101, 100, nil),
				issueCommentsResp(map[string]any{
					"pageInfo": pageInfo(false, ""),
					"nodes": []map[string]any{
						issueCommentNode(int64(5100001), "2026-04-12T11:00:00Z",
							userAuthor("alice", 2001)),
					},
				}),
			),
			// PR #102 (newer updated_at) second.
			gqlInteraction(t, prconvo.ReviewsQuery,
				vars("karnstack", "multi", 102, 100, nil),
				reviewsResp(map[string]any{
					"pageInfo": pageInfo(false, ""),
					"nodes": []map[string]any{
						reviewNode(int64(3100002), "CHANGES_REQUESTED", "2026-04-13T09:00:00Z",
							userAuthor("bob", 2010)),
					},
				}),
			),
			gqlInteraction(t, prconvo.ReviewCommentsQuery,
				vars("karnstack", "multi", 102, 100, nil),
				reviewCommentsResp(map[string]any{
					"pageInfo": pageInfo(false, ""),
					"nodes":    []map[string]any{},
				}),
			),
			gqlInteraction(t, prconvo.IssueCommentsQuery,
				vars("karnstack", "multi", 102, 100, nil),
				issueCommentsResp(map[string]any{
					"pageInfo": pageInfo(false, ""),
					"nodes": []map[string]any{
						issueCommentNode(int64(5100002), "2026-04-13T09:30:00Z",
							botAuthor("ci-bot", 2030)),
					},
				}),
			),
		}
		mustWrite(t, filepath.Join("testdata", "two_prs.json"),
			&vcr.Cassette{Version: vcr.CassetteVersion, Interactions: ix})
	})

	// truncated_thread — one PR whose review_comments response has a
	// thread with hasNextPage=true on its inlined comments connection,
	// triggering ReviewCommentsPage.Truncated=true. The runner must log
	// a warn but not fail.
	t.Run("truncated_thread", func(t *testing.T) {
		ix := []vcr.Interaction{
			gqlInteraction(t, prconvo.ReviewsQuery,
				vars("karnstack", "trunc", 101, 100, nil),
				reviewsResp(map[string]any{
					"pageInfo": pageInfo(false, ""),
					"nodes":    []map[string]any{},
				}),
			),
			gqlInteraction(t, prconvo.ReviewCommentsQuery,
				vars("karnstack", "trunc", 101, 100, nil),
				reviewCommentsResp(map[string]any{
					"pageInfo": pageInfo(false, ""),
					"nodes": []map[string]any{
						{
							"comments": map[string]any{
								// >100 comments in a thread → truncated.
								"pageInfo": map[string]any{"hasNextPage": true},
								"nodes": []map[string]any{
									reviewCommentNode(int64(4200001), "2026-04-14T10:00:00Z",
										userAuthor("alice", 2001)),
								},
							},
						},
					},
				}),
			),
			gqlInteraction(t, prconvo.IssueCommentsQuery,
				vars("karnstack", "trunc", 101, 100, nil),
				issueCommentsResp(map[string]any{
					"pageInfo": pageInfo(false, ""),
					"nodes":    []map[string]any{},
				}),
			),
		}
		mustWrite(t, filepath.Join("testdata", "truncated_thread.json"),
			&vcr.Cassette{Version: vcr.CassetteVersion, Interactions: ix})
	})

	// since_recent — one PR (#102) newer than a pre-seeded cursor; the
	// older PR (#101) must be filtered by the DB query and never queried.
	// Cassette only contains #102's three calls.
	t.Run("since_recent", func(t *testing.T) {
		ix := []vcr.Interaction{
			gqlInteraction(t, prconvo.ReviewsQuery,
				vars("karnstack", "recent", 102, 100, nil),
				reviewsResp(map[string]any{
					"pageInfo": pageInfo(false, ""),
					"nodes": []map[string]any{
						reviewNode(int64(3300001), "APPROVED", "2026-04-15T10:00:00Z",
							userAuthor("alice", 2001)),
					},
				}),
			),
			gqlInteraction(t, prconvo.ReviewCommentsQuery,
				vars("karnstack", "recent", 102, 100, nil),
				reviewCommentsResp(map[string]any{
					"pageInfo": pageInfo(false, ""),
					"nodes":    []map[string]any{},
				}),
			),
			gqlInteraction(t, prconvo.IssueCommentsQuery,
				vars("karnstack", "recent", 102, 100, nil),
				issueCommentsResp(map[string]any{
					"pageInfo": pageInfo(false, ""),
					"nodes":    []map[string]any{},
				}),
			),
		}
		mustWrite(t, filepath.Join("testdata", "since_recent.json"),
			&vcr.Cassette{Version: vcr.CassetteVersion, Interactions: ix})
	})

	// pr_failure — PR #1 succeeds (3 calls), PR #2's first call returns
	// a GraphQL NOT_FOUND. Establishes that a mid-repo failure stops the
	// repo's processing AND prevents cursor advancement.
	t.Run("pr_failure", func(t *testing.T) {
		ix := []vcr.Interaction{
			// PR #1 succeeds entirely.
			gqlInteraction(t, prconvo.ReviewsQuery,
				vars("karnstack", "aok", 1, 100, nil),
				reviewsResp(map[string]any{
					"pageInfo": pageInfo(false, ""),
					"nodes": []map[string]any{
						reviewNode(int64(3400001), "APPROVED", "2026-04-12T10:00:00Z",
							userAuthor("alice", 2001)),
					},
				}),
			),
			gqlInteraction(t, prconvo.ReviewCommentsQuery,
				vars("karnstack", "aok", 1, 100, nil),
				reviewCommentsResp(map[string]any{
					"pageInfo": pageInfo(false, ""),
					"nodes":    []map[string]any{},
				}),
			),
			gqlInteraction(t, prconvo.IssueCommentsQuery,
				vars("karnstack", "aok", 1, 100, nil),
				issueCommentsResp(map[string]any{
					"pageInfo": pageInfo(false, ""),
					"nodes":    []map[string]any{},
				}),
			),
			// PR #2's reviews call returns a GraphQL NOT_FOUND.
			gqlInteraction(t, prconvo.ReviewsQuery,
				vars("karnstack", "aok", 2, 100, nil),
				mustMarshal(t, map[string]any{
					"data": nil,
					"errors": []map[string]any{{
						"message": "Could not resolve to a PullRequest with the number of 2.",
						"type":    "NOT_FOUND",
						"path":    []any{"repository", "pullRequest"},
					}},
				}),
			),
		}
		mustWrite(t, filepath.Join("testdata", "pr_failure.json"),
			&vcr.Cassette{Version: vcr.CassetteVersion, Interactions: ix})
	})
}

// --- builders ---

func gqlInteraction(t *testing.T, query string, variables map[string]any, body json.RawMessage) vcr.Interaction {
	t.Helper()
	req := mustMarshal(t, map[string]any{"query": query, "variables": variables})
	return vcr.Interaction{
		Request:  vcr.Request{Method: "POST", URL: "https://api.github.com/graphql", Body: req},
		Response: vcr.Response{Status: 200, Body: body},
	}
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

func pageInfo(hasNext bool, endCursor string) map[string]any {
	return map[string]any{
		"hasNextPage": hasNext,
		"endCursor":   endCursor,
	}
}

func userAuthor(login string, id int64) map[string]any {
	return map[string]any{"__typename": "User", "login": login, "databaseId": id}
}

func botAuthor(login string, id int64) map[string]any {
	return map[string]any{"__typename": "Bot", "login": login, "databaseId": id}
}

func reviewNode(id int64, state, submittedAt string, author map[string]any) map[string]any {
	return map[string]any{
		"databaseId":  id,
		"state":       state,
		"submittedAt": submittedAt,
		"author":      author,
	}
}

func reviewCommentNode(id int64, createdAt string, author map[string]any) map[string]any {
	return map[string]any{"databaseId": id, "createdAt": createdAt, "author": author}
}

func issueCommentNode(id int64, createdAt string, author map[string]any) map[string]any {
	return map[string]any{"databaseId": id, "createdAt": createdAt, "author": author}
}

func reviewsResp(reviews map[string]any) json.RawMessage {
	return mustMarshal(nil, map[string]any{
		"data": map[string]any{
			"repository": map[string]any{
				"pullRequest": map[string]any{"reviews": reviews},
			},
		},
	})
}

func reviewCommentsResp(threads map[string]any) json.RawMessage {
	return mustMarshal(nil, map[string]any{
		"data": map[string]any{
			"repository": map[string]any{
				"pullRequest": map[string]any{"reviewThreads": threads},
			},
		},
	})
}

func issueCommentsResp(comments map[string]any) json.RawMessage {
	return mustMarshal(nil, map[string]any{
		"data": map[string]any{
			"repository": map[string]any{
				"pullRequest": map[string]any{"comments": comments},
			},
		},
	})
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
