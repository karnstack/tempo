package github

import (
	"errors"
	"fmt"
	"strings"
)

// ErrRateLimited is returned when retries are exhausted on a 429 (secondary
// rate-limit). Distinct from the per-bucket primary limit which Limiter.Wait
// handles transparently.
var ErrRateLimited = errors.New("github: rate limited")

// HTTPError wraps a non-2xx, non-304 response.
type HTTPError struct {
	Status int
	Body   []byte
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("github: HTTP %d: %s", e.Status, snippet(e.Body))
}

// Is allows errors.Is(err, ErrRateLimited) to succeed for 429 responses.
func (e *HTTPError) Is(target error) bool {
	return target == ErrRateLimited && e.Status == 429
}

// GraphQLError is returned by Client.GraphQL when the response carries
// errors[] (regardless of HTTP status — GraphQL signals app errors in 200).
type GraphQLError struct {
	Errors []GraphQLErrorEntry
}

type GraphQLErrorEntry struct {
	Message string `json:"message"`
	Type    string `json:"type,omitempty"`
	Path    []any  `json:"path,omitempty"`
}

func (e *GraphQLError) Error() string {
	msgs := make([]string, 0, len(e.Errors))
	for _, x := range e.Errors {
		msgs = append(msgs, x.Message)
	}
	return "github: graphql: " + strings.Join(msgs, "; ")
}

func snippet(b []byte) string {
	const max = 200
	if len(b) <= max {
		return string(b)
	}
	return string(b[:max]) + "…"
}
