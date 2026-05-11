// Package vcr is a tiny, stdlib-only HTTP recorder/replayer for tempo's
// GitHub client tests.
//
// A Transport wraps an inner http.RoundTripper. In ModeReplay (the default
// for tests) it serves canned responses from a JSON cassette and fails loud
// on a miss. In ModeRecord it forwards to the inner transport, captures the
// response, redacts the Authorization header, filters response headers to a
// stable allow-list, and writes the result to the cassette file on Close.
// ModeAuto records if the cassette is missing and replays otherwise.
//
// Usage in a fetcher test:
//
//	tr, err := vcr.NewTransport("testdata/list_prs.json", vcr.ModeReplay)
//	if err != nil { t.Fatal(err) }
//	t.Cleanup(func() {
//	    _ = tr.Close()
//	    if err := tr.Done(); err != nil { t.Fatal(err) }
//	})
//	c := github.New("test-token", github.WithHTTPClient(&http.Client{Transport: tr}))
//
// Re-recording: run the test with the `record` build tag, which the demo test
// in this package documents. Cassettes are committed under the fetcher
// package's testdata/ directory.
package vcr
