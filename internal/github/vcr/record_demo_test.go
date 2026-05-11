//go:build record

// This file only compiles under `go test -tags=record`. It exists to:
//
//  1. Pin the documented re-record entry point for fetcher cassettes
//     (TestRecord_Demo here is the template fetcher tests can copy).
//  2. Keep the record-mode wiring honest — if the API drifts, this build
//     breaks loudly under CI's `-tags=record` smoke instead of silently
//     rotting until someone tries to re-record real GitHub traffic.
//
// It intentionally hits a local httptest.Server, not api.github.com, so it
// remains safe to run anywhere. Real fetcher re-records swap fakeUpstream
// for github.New(token, ...) plus WithHTTPClient(vcrTransport).

package vcr

import (
	"io"
	"net/http"
	"path/filepath"
	"testing"
)

func TestRecord_Demo(t *testing.T) {
	srv := fakeUpstream(t)
	defer srv.Close()

	dir := t.TempDir()
	path := filepath.Join(dir, "demo.json")
	tr, err := NewTransport(path, ModeRecord)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := tr.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	})

	req, _ := http.NewRequest("GET", srv.URL+"/repos/x/y", nil)
	req.Header.Set("Authorization", "Bearer fake-pat-do-not-use")
	resp, err := tr.RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip: %v", err)
	}
	_, _ = io.ReadAll(resp.Body)
	_ = resp.Body.Close()
}
