package deployments

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
		Environment: "production",
		PerPage:     999,
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
	if page.ETag != `W/"dep-abc"` {
		t.Errorf("ETag = %q, want %q", page.ETag, `W/"dep-abc"`)
	}
	if got, want := len(page.Deployments), 3; got != want {
		t.Fatalf("len(Deployments) = %d, want %d", got, want)
	}

	// Deployment 0: User creator (alice).
	d := page.Deployments[0]
	if d.GHID != 5001 {
		t.Errorf("Deployments[0].GHID = %d, want 5001", d.GHID)
	}
	if d.SHA != "a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1" {
		t.Errorf("Deployments[0].SHA = %q", d.SHA)
	}
	if d.Ref != "main" {
		t.Errorf("Deployments[0].Ref = %q, want main", d.Ref)
	}
	if d.Task != "deploy" {
		t.Errorf("Deployments[0].Task = %q, want deploy", d.Task)
	}
	if d.Environment != "production" {
		t.Errorf("Deployments[0].Environment = %q", d.Environment)
	}
	if d.Description != "Auto-deploy from main" {
		t.Errorf("Deployments[0].Description = %q", d.Description)
	}
	if d.Creator != (prs.Author{GHID: 2001, Login: "alice", Type: "User"}) {
		t.Errorf("Deployments[0].Creator = %+v, want User{2001, alice}", d.Creator)
	}
	if !d.CreatedAt.Equal(mustParse(t, "2026-04-12T10:00:00Z")) {
		t.Errorf("Deployments[0].CreatedAt = %v", d.CreatedAt)
	}
	if !d.UpdatedAt.Equal(mustParse(t, "2026-04-12T10:00:00Z")) {
		t.Errorf("Deployments[0].UpdatedAt = %v", d.UpdatedAt)
	}

	// Deployment 1: Bot creator (deploybot); UpdatedAt strictly after CreatedAt.
	d = page.Deployments[1]
	if d.GHID != 5002 {
		t.Errorf("Deployments[1].GHID = %d, want 5002", d.GHID)
	}
	if d.Task != "deploy:hotfix" {
		t.Errorf("Deployments[1].Task = %q, want deploy:hotfix", d.Task)
	}
	if d.Creator != (prs.Author{GHID: 3001, Login: "deploybot", Type: "Bot"}) {
		t.Errorf("Deployments[1].Creator = %+v, want Bot{3001, deploybot}", d.Creator)
	}
	if !d.CreatedAt.Equal(mustParse(t, "2026-04-11T08:00:00Z")) {
		t.Errorf("Deployments[1].CreatedAt = %v", d.CreatedAt)
	}
	if !d.UpdatedAt.Equal(mustParse(t, "2026-04-11T08:05:00Z")) {
		t.Errorf("Deployments[1].UpdatedAt = %v", d.UpdatedAt)
	}
	if d.CreatedAt.Equal(d.UpdatedAt) {
		t.Errorf("Deployments[1] CreatedAt == UpdatedAt, want distinct")
	}

	// Deployment 2: null creator -> Ghost; null description -> "".
	d = page.Deployments[2]
	if d.GHID != 5003 {
		t.Errorf("Deployments[2].GHID = %d, want 5003", d.GHID)
	}
	if d.Description != "" {
		t.Errorf("Deployments[2].Description = %q, want \"\" (null collapses to zero string)", d.Description)
	}
	if d.Creator != (prs.Author{Type: "Ghost"}) {
		t.Errorf("Deployments[2].Creator = %+v, want Ghost", d.Creator)
	}
}

func TestFetch_NotModified(t *testing.T) {
	f := New(newReplayClient(t, "testdata/list_not_modified.json"))

	page, err := f.Fetch(context.Background(), "karnstack", "tempo", FetchOptions{
		Environment: "production",
		PerPage:     100,
		ETag:        `W/"dep-abc"`,
	})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if !page.NotModified {
		t.Errorf("NotModified = false, want true")
	}
	if len(page.Deployments) != 0 {
		t.Errorf("len(Deployments) = %d, want 0", len(page.Deployments))
	}
	if page.HasNext {
		t.Errorf("HasNext = true, want false")
	}
	if page.NextPage != 0 {
		t.Errorf("NextPage = %d, want 0", page.NextPage)
	}
	// Server omitted ETag on 304 -> fetcher echoes caller's value.
	if page.ETag != `W/"dep-abc"` {
		t.Errorf("ETag = %q, want caller-echo %q", page.ETag, `W/"dep-abc"`)
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
