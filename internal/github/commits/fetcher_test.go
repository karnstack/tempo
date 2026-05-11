package commits

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/karnstack/tempo/internal/github"
	"github.com/karnstack/tempo/internal/github/prs"
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

	since := mustParse(t, "2026-03-01T00:00:00Z")
	// PerPage=999 exercises the 100-cap clamp (cassette is recorded with per_page=100).
	page, err := f.Fetch(context.Background(), "karnstack", "tempo", FetchOptions{
		Since:   since,
		PerPage: 999,
	})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if page.NotModified {
		t.Errorf("NotModified = true, want false")
	}
	if !page.HasNext {
		t.Errorf("HasNext = false, want true")
	}
	if page.NextPage != 2 {
		t.Errorf("NextPage = %d, want 2", page.NextPage)
	}
	if page.ETag != `W/"abc123"` {
		t.Errorf("ETag = %q, want %q", page.ETag, `W/"abc123"`)
	}
	if got, want := len(page.Commits), 3; got != want {
		t.Fatalf("len(Commits) = %d, want %d", got, want)
	}

	// Commit 0: User author + User committer; AuthoredAt == CommittedAt.
	c := page.Commits[0]
	if c.SHA != "a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1" {
		t.Errorf("Commits[0].SHA = %q", c.SHA)
	}
	if c.Message != "feat(charts): cycle-time line" {
		t.Errorf("Commits[0].Message = %q", c.Message)
	}
	if c.Author != (prs.Author{GHID: 2001, Login: "alice", Type: "User"}) {
		t.Errorf("Commits[0].Author = %+v, want User{2001, alice}", c.Author)
	}
	if c.Committer != (prs.Author{GHID: 2001, Login: "alice", Type: "User"}) {
		t.Errorf("Commits[0].Committer = %+v, want User{2001, alice}", c.Committer)
	}
	if !c.AuthoredAt.Equal(mustParse(t, "2026-04-12T10:00:00Z")) {
		t.Errorf("Commits[0].AuthoredAt = %v", c.AuthoredAt)
	}
	if !c.CommittedAt.Equal(mustParse(t, "2026-04-12T10:00:00Z")) {
		t.Errorf("Commits[0].CommittedAt = %v", c.CommittedAt)
	}

	// Commit 1: Bot author + User committer; AuthoredAt != CommittedAt (rebase).
	c = page.Commits[1]
	if c.SHA != "b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2" {
		t.Errorf("Commits[1].SHA = %q", c.SHA)
	}
	if c.Author != (prs.Author{GHID: 2002, Login: "renovate[bot]", Type: "Bot"}) {
		t.Errorf("Commits[1].Author = %+v, want Bot{2002, renovate[bot]}", c.Author)
	}
	if c.Committer != (prs.Author{GHID: 2001, Login: "alice", Type: "User"}) {
		t.Errorf("Commits[1].Committer = %+v, want User{2001, alice}", c.Committer)
	}
	if !c.AuthoredAt.Equal(mustParse(t, "2026-04-11T08:00:00Z")) {
		t.Errorf("Commits[1].AuthoredAt = %v", c.AuthoredAt)
	}
	if !c.CommittedAt.Equal(mustParse(t, "2026-04-11T08:15:00Z")) {
		t.Errorf("Commits[1].CommittedAt = %v", c.CommittedAt)
	}
	if c.AuthoredAt.Equal(c.CommittedAt) {
		t.Errorf("Commits[1] AuthoredAt == CommittedAt, want distinct")
	}

	// Commit 2: null author + null committer -> Ghost on both ends.
	c = page.Commits[2]
	if c.SHA != "c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3" {
		t.Errorf("Commits[2].SHA = %q", c.SHA)
	}
	if c.Author != (prs.Author{Type: "Ghost"}) {
		t.Errorf("Commits[2].Author = %+v, want Ghost", c.Author)
	}
	if c.Committer != (prs.Author{Type: "Ghost"}) {
		t.Errorf("Commits[2].Committer = %+v, want Ghost", c.Committer)
	}
}

func TestFetch_NotModified(t *testing.T) {
	f := New(newReplayClient(t, "testdata/list_not_modified.json"))

	since := mustParse(t, "2026-03-01T00:00:00Z")
	page, err := f.Fetch(context.Background(), "karnstack", "tempo", FetchOptions{
		Since:   since,
		PerPage: 100,
		ETag:    `W/"abc123"`,
	})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if !page.NotModified {
		t.Errorf("NotModified = false, want true")
	}
	if len(page.Commits) != 0 {
		t.Errorf("len(Commits) = %d, want 0", len(page.Commits))
	}
	if page.HasNext {
		t.Errorf("HasNext = true, want false")
	}
	if page.NextPage != 0 {
		t.Errorf("NextPage = %d, want 0", page.NextPage)
	}
	// Server omitted ETag on 304 -> fetcher echoes caller's value.
	if page.ETag != `W/"abc123"` {
		t.Errorf("ETag = %q, want caller-echo %q", page.ETag, `W/"abc123"`)
	}
}

func TestFetch_HTTPError(t *testing.T) {
	f := New(newReplayClient(t, "testdata/list_http_error.json"))

	_, err := f.Fetch(context.Background(), "ghost-org", "missing-repo", FetchOptions{
		PerPage: 100,
	})
	if err == nil {
		t.Fatal("Fetch err = nil, want *github.HTTPError")
	}
	var herr *github.HTTPError
	if !errors.As(err, &herr) {
		t.Fatalf("err = %v (%T), want *github.HTTPError", err, err)
	}
	if herr.Status != http.StatusNotFound {
		t.Errorf("status = %d, want 404", herr.Status)
	}
}
