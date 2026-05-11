package releases

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

	// PerPage=999 exercises the 100-cap clamp (cassette recorded with per_page=100).
	page, err := f.Fetch(context.Background(), "karnstack", "tempo", FetchOptions{
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
	if page.ETag != `W/"rel-abc"` {
		t.Errorf("ETag = %q, want %q", page.ETag, `W/"rel-abc"`)
	}
	if got, want := len(page.Releases), 3; got != want {
		t.Fatalf("len(Releases) = %d, want %d", got, want)
	}

	// Release 0: v1.0.0 stable. User author, published, not draft/prerelease.
	r := page.Releases[0]
	if r.GHID != 7001 {
		t.Errorf("Releases[0].GHID = %d, want 7001", r.GHID)
	}
	if r.TagName != "v1.0.0" {
		t.Errorf("Releases[0].TagName = %q", r.TagName)
	}
	if r.Name != "v1.0.0 — first stable" {
		t.Errorf("Releases[0].Name = %q", r.Name)
	}
	if r.Draft {
		t.Errorf("Releases[0].Draft = true, want false")
	}
	if r.Prerelease {
		t.Errorf("Releases[0].Prerelease = true, want false")
	}
	if r.TargetCommitish != "main" {
		t.Errorf("Releases[0].TargetCommitish = %q", r.TargetCommitish)
	}
	if r.Body != "First stable release." {
		t.Errorf("Releases[0].Body = %q", r.Body)
	}
	if r.Author != (prs.Author{GHID: 2001, Login: "alice", Type: "User"}) {
		t.Errorf("Releases[0].Author = %+v, want User{2001, alice}", r.Author)
	}
	if !r.CreatedAt.Equal(mustParse(t, "2026-04-12T10:00:00Z")) {
		t.Errorf("Releases[0].CreatedAt = %v", r.CreatedAt)
	}
	if !r.PublishedAt.Equal(mustParse(t, "2026-04-12T10:05:00Z")) {
		t.Errorf("Releases[0].PublishedAt = %v", r.PublishedAt)
	}

	// Release 1: prerelease=true (RC).
	r = page.Releases[1]
	if r.GHID != 7002 {
		t.Errorf("Releases[1].GHID = %d, want 7002", r.GHID)
	}
	if r.TagName != "v1.1.0-rc.1" {
		t.Errorf("Releases[1].TagName = %q", r.TagName)
	}
	if !r.Prerelease {
		t.Errorf("Releases[1].Prerelease = false, want true")
	}
	if r.Draft {
		t.Errorf("Releases[1].Draft = true, want false")
	}
	if r.Author != (prs.Author{GHID: 2002, Login: "bob", Type: "User"}) {
		t.Errorf("Releases[1].Author = %+v, want User{2002, bob}", r.Author)
	}
	if r.PublishedAt.IsZero() {
		t.Errorf("Releases[1].PublishedAt is zero, want set")
	}

	// Release 2: draft=true, null author -> Ghost, null published_at -> zero time.
	r = page.Releases[2]
	if r.GHID != 7003 {
		t.Errorf("Releases[2].GHID = %d, want 7003", r.GHID)
	}
	if !r.Draft {
		t.Errorf("Releases[2].Draft = false, want true")
	}
	if r.Author != (prs.Author{Type: "Ghost"}) {
		t.Errorf("Releases[2].Author = %+v, want Ghost", r.Author)
	}
	if !r.PublishedAt.IsZero() {
		t.Errorf("Releases[2].PublishedAt = %v, want zero (drafts have null published_at)", r.PublishedAt)
	}
	if !r.CreatedAt.Equal(mustParse(t, "2026-04-10T12:30:00Z")) {
		t.Errorf("Releases[2].CreatedAt = %v", r.CreatedAt)
	}
}

func TestFetch_NotModified(t *testing.T) {
	f := New(newReplayClient(t, "testdata/list_not_modified.json"))

	page, err := f.Fetch(context.Background(), "karnstack", "tempo", FetchOptions{
		PerPage: 100,
		ETag:    `W/"rel-abc"`,
	})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if !page.NotModified {
		t.Errorf("NotModified = false, want true")
	}
	if len(page.Releases) != 0 {
		t.Errorf("len(Releases) = %d, want 0", len(page.Releases))
	}
	if page.HasNext {
		t.Errorf("HasNext = true, want false")
	}
	if page.NextPage != 0 {
		t.Errorf("NextPage = %d, want 0", page.NextPage)
	}
	// Server omitted ETag on 304 -> fetcher echoes caller's value.
	if page.ETag != `W/"rel-abc"` {
		t.Errorf("ETag = %q, want caller-echo %q", page.ETag, `W/"rel-abc"`)
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
