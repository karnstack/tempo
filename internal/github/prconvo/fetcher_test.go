package prconvo

import (
	"context"
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

func TestFetchReviews_Page(t *testing.T) {
	f := New(newReplayClient(t, "testdata/reviews_page.json"))

	// first=999 exercises the 100-cap clamp (cassette recorded with first=100).
	page, err := f.FetchReviews(context.Background(), "karnstack", "tempo", 101, "", 999)
	if err != nil {
		t.Fatalf("FetchReviews: %v", err)
	}
	if !page.HasNext {
		t.Errorf("HasNext = false, want true")
	}
	if page.EndCursor != "Y3Vyc29yOnJ2LTQ=" {
		t.Errorf("EndCursor = %q, want Y3Vyc29yOnJ2LTQ=", page.EndCursor)
	}
	if got, want := len(page.Reviews), 4; got != want {
		t.Fatalf("len(Reviews) = %d, want %d", got, want)
	}

	r := page.Reviews[0]
	if r.GHID != 3000001 || r.State != "APPROVED" {
		t.Errorf("Reviews[0] = %+v, want GHID=3000001 State=APPROVED", r)
	}
	if r.SubmittedAt == nil || !r.SubmittedAt.Equal(mustParse(t, "2026-04-11T12:00:00Z")) {
		t.Errorf("Reviews[0].SubmittedAt = %v, want 2026-04-11T12:00:00Z", r.SubmittedAt)
	}
	if r.Author != (prs.Author{GHID: 2001, Login: "alice", Type: "User"}) {
		t.Errorf("Reviews[0].Author = %+v, want User{2001, alice}", r.Author)
	}

	if got, want := page.Reviews[1].State, "CHANGES_REQUESTED"; got != want {
		t.Errorf("Reviews[1].State = %q, want %q", got, want)
	}

	r = page.Reviews[2]
	if r.State != "COMMENTED" {
		t.Errorf("Reviews[2].State = %q, want COMMENTED", r.State)
	}
	if r.Author != (prs.Author{GHID: 2099, Login: "review-bot", Type: "Bot"}) {
		t.Errorf("Reviews[2].Author = %+v, want Bot{2099, review-bot}", r.Author)
	}

	r = page.Reviews[3]
	if r.State != "DISMISSED" {
		t.Errorf("Reviews[3].State = %q, want DISMISSED", r.State)
	}
	if r.Author != (prs.Author{Type: "Ghost"}) {
		t.Errorf("Reviews[3].Author = %+v, want Ghost{}", r.Author)
	}
}

func TestFetchReviewComments_Page(t *testing.T) {
	f := New(newReplayClient(t, "testdata/review_comments_page.json"))

	page, err := f.FetchReviewComments(context.Background(), "karnstack", "tempo", 101, "", 100)
	if err != nil {
		t.Fatalf("FetchReviewComments: %v", err)
	}
	if page.HasNext {
		t.Errorf("HasNext = true, want false")
	}
	if page.EndCursor != "Y3Vyc29yOnRoLTI=" {
		t.Errorf("EndCursor = %q, want Y3Vyc29yOnRoLTI=", page.EndCursor)
	}
	if !page.Truncated {
		t.Errorf("Truncated = false, want true (thread 2 overflowed)")
	}
	if got, want := len(page.Comments), 3; got != want {
		t.Fatalf("len(Comments) = %d, want %d (thread1=2 + thread2=1)", got, want)
	}

	c := page.Comments[0]
	if c.GHID != 4000001 {
		t.Errorf("Comments[0].GHID = %d, want 4000001", c.GHID)
	}
	if !c.CreatedAt.Equal(mustParse(t, "2026-04-11T09:15:00Z")) {
		t.Errorf("Comments[0].CreatedAt = %v, want 2026-04-11T09:15:00Z", c.CreatedAt)
	}
	if c.Author != (prs.Author{GHID: 2001, Login: "alice", Type: "User"}) {
		t.Errorf("Comments[0].Author = %+v, want User{2001, alice}", c.Author)
	}
	if page.Comments[1].Author.Login != "carol" {
		t.Errorf("Comments[1].Author.Login = %q, want carol", page.Comments[1].Author.Login)
	}

	c = page.Comments[2]
	if c.Author != (prs.Author{Type: "Ghost"}) {
		t.Errorf("Comments[2].Author = %+v, want Ghost{}", c.Author)
	}
}

func TestFetchIssueComments_Page(t *testing.T) {
	f := New(newReplayClient(t, "testdata/issue_comments_page.json"))

	page, err := f.FetchIssueComments(context.Background(), "karnstack", "tempo", 101, "", 100)
	if err != nil {
		t.Fatalf("FetchIssueComments: %v", err)
	}
	if page.HasNext {
		t.Errorf("HasNext = true, want false")
	}
	if page.EndCursor != "Y3Vyc29yOmljLTQ=" {
		t.Errorf("EndCursor = %q, want Y3Vyc29yOmljLTQ=", page.EndCursor)
	}
	if got, want := len(page.Comments), 4; got != want {
		t.Fatalf("len(Comments) = %d, want %d", got, want)
	}

	// Author type spread: User, Bot, Mannequin, Ghost.
	wantTypes := []string{"User", "Bot", "Mannequin", "Ghost"}
	for i, want := range wantTypes {
		if got := page.Comments[i].Author.Type; got != want {
			t.Errorf("Comments[%d].Author.Type = %q, want %q", i, got, want)
		}
	}
	if page.Comments[2].Author != (prs.Author{GHID: 2040, Login: "old-dan", Type: "Mannequin"}) {
		t.Errorf("Comments[2].Author = %+v, want Mannequin{2040, old-dan}", page.Comments[2].Author)
	}
	if page.Comments[3].Author.GHID != 0 || page.Comments[3].Author.Login != "" {
		t.Errorf("Comments[3].Author = %+v, want Ghost zero-values", page.Comments[3].Author)
	}
}
