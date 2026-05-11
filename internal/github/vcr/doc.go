// Package vcr is a tiny, stdlib-only HTTP recorder/replayer for tempo's
// GitHub client tests.
//
// # Modes
//
//   - ModeReplay (default): serve canned responses from the cassette and
//     fail loud on any miss or extra call. Wire this into CI.
//   - ModeRecord: forward to the inner transport, capture the response,
//     redact and persist on Close.
//   - ModeAuto: replay if the cassette file exists, record otherwise.
//     Convenient for first-time fixture authoring.
//
// The env override TEMPO_VCR=record|replay|auto plumbs through ModeFromEnv
// without recompiling.
//
// # Wiring a test
//
//	tr, err := vcr.NewTransport("testdata/list_prs.json", vcr.ModeReplay)
//	if err != nil { t.Fatal(err) }
//	t.Cleanup(func() {
//	    if err := tr.Close(); err != nil { t.Error(err) }
//	    if err := tr.Done();  err != nil { t.Error(err) }
//	})
//	c := github.New("test-token", github.WithHTTPClient(&http.Client{Transport: tr}))
//
// Done returns an error if a replay test left interactions unconsumed, which
// catches stale cassettes when the call set shrinks. Close flushes pending
// writes in record mode and is a no-op otherwise.
//
// # Re-recording
//
// Run the test with the `record` build tag, which gates record-mode tests in
// fetcher packages:
//
//	GITHUB_TOKEN=ghp_... go test -tags=record ./internal/github/...
//
// See record_demo_test.go in this package for the template. CI runs without
// the tag so production tests stay hermetic.
//
// # What's redacted, what isn't
//
// On record, the request Authorization header is rewritten to
// "Bearer REDACTED". Response headers are filtered to a stable allow-list
// (Cache-Control, Content-Type, ETag, Last-Modified, Retry-After, and the
// X-RateLimit-* family) so cassettes don't churn on per-request server
// metadata. Request and response bodies are NOT scrubbed — recorded
// repository names, PR titles, and login handles are intentionally faithful
// so tests exercise realistic data. Use a throwaway PAT or anonymised repos
// when recording sensitive material.
//
// # Cassette layout
//
// Cassettes are JSON, one file per scenario, committed under the fetcher
// package's testdata/ directory (e.g. internal/github/prs/testdata/list.json).
// Bodies are stored as json.RawMessage so JSON payloads embed inline and
// stay diff-readable; non-JSON bodies fall back to a JSON-encoded string.
// On Load, JSON bodies are compacted back into wire form so replay returns
// byte-equal output to what the upstream sent.
//
// # Matching
//
// Matching is ordered and single-shot: the cursor advances one interaction
// per RoundTrip. The match key ignores host and scheme so cassettes recorded
// against api.github.com replay against any base URL the test passes the
// client; it normalises GraphQL request bodies through Unmarshal/Marshal so
// whitespace and JSON key order don't break matches. Different query
// parameters and different GraphQL variables produce different keys, as
// expected.
package vcr
