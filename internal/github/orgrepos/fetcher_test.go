package orgrepos

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

func TestFetch_Page(t *testing.T) {
	f := New(newReplayClient(t, "testdata/list_page.json"))

	// PerPage=999 exercises the 100-cap clamp (cassette recorded with
	// per_page=100).
	page, err := f.Fetch(context.Background(), "karnstack", FetchOptions{
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
	if page.ETag != `W/"orgrepos-abc"` {
		t.Errorf("ETag = %q, want %q", page.ETag, `W/"orgrepos-abc"`)
	}
	if got, want := len(page.Repos), 3; got != want {
		t.Fatalf("len(Repos) = %d, want %d", got, want)
	}

	// Repo 0: regular public on `main`.
	r := page.Repos[0]
	if r.GHID != 9001 {
		t.Errorf("Repos[0].GHID = %d, want 9001", r.GHID)
	}
	if r.Owner != "karnstack" {
		t.Errorf("Repos[0].Owner = %q, want karnstack", r.Owner)
	}
	if r.Name != "tempo" {
		t.Errorf("Repos[0].Name = %q, want tempo", r.Name)
	}
	if r.DefaultBranch != "main" {
		t.Errorf("Repos[0].DefaultBranch = %q, want main", r.DefaultBranch)
	}
	if r.Archived {
		t.Errorf("Repos[0].Archived = true, want false")
	}
	if r.Fork {
		t.Errorf("Repos[0].Fork = true, want false")
	}
	if r.Private {
		t.Errorf("Repos[0].Private = true, want false")
	}

	// Repo 1: archived=true, default_branch=master.
	r = page.Repos[1]
	if r.GHID != 9002 {
		t.Errorf("Repos[1].GHID = %d, want 9002", r.GHID)
	}
	if r.Name != "legacy-archive" {
		t.Errorf("Repos[1].Name = %q, want legacy-archive", r.Name)
	}
	if !r.Archived {
		t.Errorf("Repos[1].Archived = false, want true")
	}
	if r.DefaultBranch != "master" {
		t.Errorf("Repos[1].DefaultBranch = %q, want master", r.DefaultBranch)
	}
	if r.Fork {
		t.Errorf("Repos[1].Fork = true, want false")
	}

	// Repo 2: fork=true.
	r = page.Repos[2]
	if r.GHID != 9003 {
		t.Errorf("Repos[2].GHID = %d, want 9003", r.GHID)
	}
	if r.Name != "forked-toy" {
		t.Errorf("Repos[2].Name = %q, want forked-toy", r.Name)
	}
	if !r.Fork {
		t.Errorf("Repos[2].Fork = false, want true")
	}
	if r.Archived {
		t.Errorf("Repos[2].Archived = true, want false")
	}
}

func TestFetch_NotModified(t *testing.T) {
	f := New(newReplayClient(t, "testdata/list_not_modified.json"))

	page, err := f.Fetch(context.Background(), "karnstack", FetchOptions{
		PerPage: 100,
		ETag:    `W/"orgrepos-abc"`,
	})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if !page.NotModified {
		t.Errorf("NotModified = false, want true")
	}
	if len(page.Repos) != 0 {
		t.Errorf("len(Repos) = %d, want 0", len(page.Repos))
	}
	if page.HasNext {
		t.Errorf("HasNext = true, want false")
	}
	if page.NextPage != 0 {
		t.Errorf("NextPage = %d, want 0", page.NextPage)
	}
	// Server omitted ETag on 304 -> fetcher echoes caller's value.
	if page.ETag != `W/"orgrepos-abc"` {
		t.Errorf("ETag = %q, want caller-echo %q", page.ETag, `W/"orgrepos-abc"`)
	}
}

func TestFetch_HTTPError(t *testing.T) {
	f := New(newReplayClient(t, "testdata/list_http_error.json"))

	_, err := f.Fetch(context.Background(), "ghost-org", FetchOptions{
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
