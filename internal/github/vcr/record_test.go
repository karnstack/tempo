package vcr

import (
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

// fakeUpstream returns an httptest.Server that serves a couple of known
// responses for record-mode tests. It expects a single GET /repos/x/y and a
// single POST /graphql.
func fakeUpstream(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Headers the allow-list should preserve, and some it should drop.
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-RateLimit-Remaining", "4998")
		w.Header().Set("X-RateLimit-Reset", "1700000000")
		w.Header().Set("ETag", `"abc"`)
		w.Header().Set("X-GitHub-Request-Id", "should-be-dropped")
		w.Header().Set("Set-Cookie", "should-be-dropped")
		switch {
		case r.Method == "GET" && r.URL.Path == "/repos/x/y":
			_, _ = io.WriteString(w, `{"name":"y"}`)
		case r.Method == "POST" && r.URL.Path == "/graphql":
			_, _ = io.WriteString(w, `{"data":{"viewer":{"login":"octocat"}}}`)
		default:
			http.NotFound(w, r)
		}
	}))
}

func TestRecordWritesCassetteAndScrubs(t *testing.T) {
	srv := fakeUpstream(t)
	defer srv.Close()

	dir := t.TempDir()
	path := filepath.Join(dir, "rec.json")
	tr, err := NewTransport(path, ModeRecord)
	if err != nil {
		t.Fatal(err)
	}

	// REST call.
	req, _ := http.NewRequest("GET", srv.URL+"/repos/x/y", nil)
	req.Header.Set("Authorization", "Bearer s3cr3t-pat")
	resp, err := tr.RoundTrip(req)
	if err != nil {
		t.Fatalf("record REST: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if string(body) != `{"name":"y"}` {
		t.Errorf("forwarded body = %q, want %q", body, `{"name":"y"}`)
	}

	// GraphQL call.
	gql, _ := http.NewRequest("POST", srv.URL+"/graphql",
		strings.NewReader(`{"query":"{viewer{login}}","variables":{}}`))
	gql.Header.Set("Authorization", "Bearer s3cr3t-pat")
	gql.Header.Set("Content-Type", "application/json")
	resp2, err := tr.RoundTrip(gql)
	if err != nil {
		t.Fatalf("record GraphQL: %v", err)
	}
	_, _ = io.ReadAll(resp2.Body)
	_ = resp2.Body.Close()

	if err := tr.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Reload and inspect.
	c, err := LoadCassette(path)
	if err != nil {
		t.Fatalf("LoadCassette: %v", err)
	}
	if len(c.Interactions) != 2 {
		t.Fatalf("interactions = %d, want 2", len(c.Interactions))
	}

	first := c.Interactions[0]
	if auth := first.Request.Headers.Get("Authorization"); auth != "Bearer REDACTED" {
		t.Errorf("Authorization not redacted: %q", auth)
	}
	if ct := first.Response.Headers.Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type missing/wrong: %q", ct)
	}
	if rem := first.Response.Headers.Get("X-Ratelimit-Remaining"); rem != "4998" {
		t.Errorf("X-RateLimit-Remaining = %q", rem)
	}
	if leaked := first.Response.Headers.Get("X-Github-Request-Id"); leaked != "" {
		t.Errorf("non-allow-listed header leaked: %q", leaked)
	}
	if leaked := first.Response.Headers.Get("Set-Cookie"); leaked != "" {
		t.Errorf("Set-Cookie leaked: %q", leaked)
	}

	// Body of REST call (post-pretty-print) still parses to the expected JSON.
	got := decodeBody(first.Response.Body)
	if !strings.Contains(string(got), `"y"`) {
		t.Errorf("recorded REST body = %s", got)
	}

	// GraphQL request body recorded.
	gqlBody := decodeBody(c.Interactions[1].Request.Body)
	if !strings.Contains(string(gqlBody), "viewer") {
		t.Errorf("recorded GraphQL request body missing query: %s", gqlBody)
	}
}

func TestRecordThenReplayRoundTrip(t *testing.T) {
	srv := fakeUpstream(t)
	defer srv.Close()

	dir := t.TempDir()
	path := filepath.Join(dir, "rt.json")

	// Phase 1: record.
	rec, err := NewTransport(path, ModeRecord)
	if err != nil {
		t.Fatal(err)
	}
	req, _ := http.NewRequest("GET", srv.URL+"/repos/x/y", nil)
	req.Header.Set("Authorization", "Bearer s3cr3t-pat")
	resp, err := rec.RoundTrip(req)
	if err != nil {
		t.Fatalf("record: %v", err)
	}
	wantBody, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err := rec.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Phase 2: tear down upstream — replay must NOT hit network.
	srv.Close()

	// Phase 3: replay.
	rep, err := NewTransport(path, ModeReplay)
	if err != nil {
		t.Fatalf("replay open: %v", err)
	}
	req2, _ := http.NewRequest("GET", srv.URL+"/repos/x/y", nil)
	resp2, err := rep.RoundTrip(req2)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	gotBody, _ := io.ReadAll(resp2.Body)
	_ = resp2.Body.Close()
	if string(gotBody) != string(wantBody) {
		t.Errorf("body drift\n  record: %s\n  replay: %s", wantBody, gotBody)
	}
	if err := rep.Done(); err != nil {
		t.Errorf("Done: %v", err)
	}
}

func TestRecordInnerTransportOverride(t *testing.T) {
	// Sanity check that WithInnerTransport is honoured — install a stub that
	// records the call but never touches the network.
	stub := &stubRT{resp: &http.Response{
		StatusCode: 201,
		Header:     http.Header{"Content-Type": []string{"text/plain"}},
		Body:       io.NopCloser(strings.NewReader("ok")),
	}}
	dir := t.TempDir()
	path := filepath.Join(dir, "c.json")
	tr, err := NewTransport(path, ModeRecord, WithInnerTransport(stub))
	if err != nil {
		t.Fatal(err)
	}
	req, _ := http.NewRequest("GET", "https://example.invalid/x", nil)
	resp, err := tr.RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip: %v", err)
	}
	_, _ = io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if !stub.called {
		t.Error("inner transport not invoked")
	}
}

type stubRT struct {
	called bool
	resp   *http.Response
}

func (s *stubRT) RoundTrip(req *http.Request) (*http.Response, error) {
	s.called = true
	s.resp.Request = req
	return s.resp, nil
}
