package prconvo

import (
	"context"
	"errors"
	"testing"

	"github.com/karnstack/tempo/internal/github"
)

func TestFetchReviews_GraphQLError(t *testing.T) {
	f := New(newReplayClient(t, "testdata/reviews_graphql_error.json"))

	_, err := f.FetchReviews(context.Background(), "ghost-org", "missing-repo", 1, "", 100)
	if err == nil {
		t.Fatal("FetchReviews err = nil, want *github.GraphQLError")
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
