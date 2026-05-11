package prs

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/karnstack/tempo/internal/github"
	"github.com/karnstack/tempo/internal/github/vcr"
)

func newReplayClient(t *testing.T, cassettePath string) *github.Client {
	t.Helper()
	tr, err := vcr.NewTransport(cassettePath, vcr.ModeReplay)
	if err != nil {
		t.Fatalf("vcr.NewTransport(%s): %v", cassettePath, err)
	}
	t.Cleanup(func() {
		if err := tr.Close(); err != nil {
			t.Errorf("vcr.Close: %v", err)
		}
		if err := tr.Done(); err != nil {
			t.Errorf("vcr.Done: %v", err)
		}
	})
	return github.New("test-token",
		github.WithHTTPClient(&http.Client{Transport: tr}),
		github.WithBackoff(func(int) time.Duration { return 0 }),
	)
}

func mustParse(t *testing.T, s string) time.Time {
	t.Helper()
	v, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatalf("parse %q: %v", s, err)
	}
	return v
}

func TestFetch_Page(t *testing.T) {
	f := New(newReplayClient(t, "testdata/list_page.json"))

	// Pass first=999 to exercise the 100-cap clamp (cassette is recorded with first=100).
	page, err := f.Fetch(context.Background(), "karnstack", "tempo", "", 999, time.Time{})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if !page.HasNext {
		t.Errorf("HasNext = false, want true")
	}
	if page.EndCursor != "Y3Vyc29yOjQ=" {
		t.Errorf("EndCursor = %q, want %q", page.EndCursor, "Y3Vyc29yOjQ=")
	}
	if page.ReachedSince {
		t.Errorf("ReachedSince = true, want false (no since cutoff)")
	}
	if got, want := len(page.PRs), 4; got != want {
		t.Fatalf("len(PRs) = %d, want %d", got, want)
	}

	// PR 0: merged, User author, full timestamps.
	pr := page.PRs[0]
	if pr.GHID != 1000001 || pr.Number != 101 || pr.State != "MERGED" || pr.Title != "Add cycle-time chart" {
		t.Errorf("PR[0] = %+v, mismatch on core fields", pr)
	}
	if pr.Author != (Author{GHID: 2001, Login: "alice", Type: "User"}) {
		t.Errorf("PR[0].Author = %+v, want User{2001, alice}", pr.Author)
	}
	if pr.MergedAt == nil || !pr.MergedAt.Equal(mustParse(t, "2026-04-12T15:30:00Z")) {
		t.Errorf("PR[0].MergedAt = %v, want 2026-04-12T15:30:00Z", pr.MergedAt)
	}
	if pr.ClosedAt == nil || !pr.ClosedAt.Equal(mustParse(t, "2026-04-12T15:30:00Z")) {
		t.Errorf("PR[0].ClosedAt = %v, want 2026-04-12T15:30:00Z", pr.ClosedAt)
	}
	if pr.Additions != 120 || pr.Deletions != 45 {
		t.Errorf("PR[0].Additions/Deletions = %d/%d, want 120/45", pr.Additions, pr.Deletions)
	}
	if pr.BaseRef != "main" || pr.HeadRef != "feat/cycle-time" {
		t.Errorf("PR[0] refs = %s..%s, want main..feat/cycle-time", pr.BaseRef, pr.HeadRef)
	}
	if pr.Draft {
		t.Errorf("PR[0].Draft = true, want false")
	}

	// PR 1: closed-not-merged, Bot author.
	pr = page.PRs[1]
	if pr.State != "CLOSED" {
		t.Errorf("PR[1].State = %s, want CLOSED", pr.State)
	}
	if pr.MergedAt != nil {
		t.Errorf("PR[1].MergedAt = %v, want nil", pr.MergedAt)
	}
	if pr.ClosedAt == nil {
		t.Errorf("PR[1].ClosedAt = nil, want set")
	}
	if pr.Author != (Author{GHID: 2002, Login: "renovate[bot]", Type: "Bot"}) {
		t.Errorf("PR[1].Author = %+v, want Bot{2002, renovate[bot]}", pr.Author)
	}

	// PR 2: open draft, Mannequin author.
	pr = page.PRs[2]
	if pr.State != "OPEN" || !pr.Draft {
		t.Errorf("PR[2] state/draft = %s/%v, want OPEN/true", pr.State, pr.Draft)
	}
	if pr.MergedAt != nil || pr.ClosedAt != nil {
		t.Errorf("PR[2] mergedAt/closedAt = %v/%v, want nil/nil", pr.MergedAt, pr.ClosedAt)
	}
	if pr.Author != (Author{GHID: 2003, Login: "old-bob", Type: "Mannequin"}) {
		t.Errorf("PR[2].Author = %+v, want Mannequin{2003, old-bob}", pr.Author)
	}

	// PR 3: null author → Ghost.
	pr = page.PRs[3]
	if pr.Author != (Author{Type: "Ghost"}) {
		t.Errorf("PR[3].Author = %+v, want Ghost{}", pr.Author)
	}
}

func TestFetch_SinceCutoff(t *testing.T) {
	f := New(newReplayClient(t, "testdata/list_since_cutoff.json"))

	since := mustParse(t, "2026-04-01T00:00:00Z")
	page, err := f.Fetch(context.Background(), "karnstack", "tempo", "", 100, since)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if !page.ReachedSince {
		t.Errorf("ReachedSince = false, want true")
	}
	if got, want := len(page.PRs), 2; got != want {
		t.Fatalf("len(PRs) = %d, want %d (the 2026-04-01T00:00:00Z PR must be dropped)", got, want)
	}
	if page.PRs[0].Number != 55 || page.PRs[1].Number != 54 {
		t.Errorf("kept PRs = %d, %d; want 55, 54", page.PRs[0].Number, page.PRs[1].Number)
	}
	if page.HasNext {
		t.Errorf("HasNext = true, want false")
	}
}

func TestFetch_GraphQLError(t *testing.T) {
	f := New(newReplayClient(t, "testdata/list_graphql_error.json"))

	_, err := f.Fetch(context.Background(), "ghost-org", "missing-repo", "", 100, time.Time{})
	if err == nil {
		t.Fatal("Fetch err = nil, want *github.GraphQLError")
	}
	var ge *github.GraphQLError
	if !errors.As(err, &ge) {
		t.Fatalf("err = %v (%T), want *github.GraphQLError", err, err)
	}
	if got, want := len(ge.Errors), 1; got != want {
		t.Fatalf("len(errors) = %d, want %d", got, want)
	}
	if ge.Errors[0].Type != "NOT_FOUND" {
		t.Errorf("err type = %q, want NOT_FOUND", ge.Errors[0].Type)
	}
}

