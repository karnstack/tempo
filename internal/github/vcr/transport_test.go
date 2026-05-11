package vcr

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
)

// writeCassette is a test helper that drops a cassette JSON file at path.
func writeCassette(t *testing.T, path string, c *Cassette) {
	t.Helper()
	if err := c.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}
}

func TestReplayHit(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "c.json")
	writeCassette(t, path, &Cassette{
		Interactions: []Interaction{
			{
				Request: Request{
					Method: "GET",
					URL:    "https://api.github.com/repos/octocat/hello",
				},
				Response: Response{
					Status:  200,
					Headers: http.Header{"Content-Type": []string{"application/json"}},
					Body:    encodeBody([]byte(`{"name":"hello"}`)),
				},
			},
		},
	})

	tr, err := NewTransport(path, ModeReplay)
	if err != nil {
		t.Fatal(err)
	}

	req, _ := http.NewRequest("GET", "https://api.github.com/repos/octocat/hello", nil)
	resp, err := tr.RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Errorf("status = %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	// Body comes back JSON-equivalent but may be re-indented by Save.
	if !bytes.Contains(body, []byte(`"name"`)) || !bytes.Contains(body, []byte(`"hello"`)) {
		t.Errorf("body = %s", body)
	}
	if got := resp.Header.Get("Content-Type"); got != "application/json" {
		t.Errorf("header = %q", got)
	}
	if err := tr.Done(); err != nil {
		t.Errorf("Done: %v", err)
	}
}

func TestReplayHitDifferentHostStillMatches(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "c.json")
	writeCassette(t, path, &Cassette{
		Interactions: []Interaction{
			{
				Request:  Request{Method: "GET", URL: "https://api.github.com/x"},
				Response: Response{Status: 204},
			},
		},
	})
	tr, err := NewTransport(path, ModeReplay)
	if err != nil {
		t.Fatal(err)
	}
	// Test client is pointed at a local httptest URL, not api.github.com.
	req, _ := http.NewRequest("GET", "http://127.0.0.1:1234/x", nil)
	resp, err := tr.RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 204 {
		t.Errorf("status = %d", resp.StatusCode)
	}
}

func TestReplayMissDifferentPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "c.json")
	writeCassette(t, path, &Cassette{
		Interactions: []Interaction{
			{Request: Request{Method: "GET", URL: "https://api.github.com/a"}, Response: Response{Status: 200}},
		},
	})
	tr, err := NewTransport(path, ModeReplay)
	if err != nil {
		t.Fatal(err)
	}
	req, _ := http.NewRequest("GET", "https://api.github.com/b", nil)
	if _, err := tr.RoundTrip(req); err == nil {
		t.Fatal("expected mismatch error")
	} else if !strings.Contains(err.Error(), "mismatch") {
		t.Errorf("err = %v, want mismatch", err)
	}
}

func TestReplayMissExtraCall(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "c.json")
	writeCassette(t, path, &Cassette{
		Interactions: []Interaction{
			{Request: Request{Method: "GET", URL: "https://api.github.com/a"}, Response: Response{Status: 200}},
		},
	})
	tr, err := NewTransport(path, ModeReplay)
	if err != nil {
		t.Fatal(err)
	}
	req1, _ := http.NewRequest("GET", "https://api.github.com/a", nil)
	if _, err := tr.RoundTrip(req1); err != nil {
		t.Fatalf("first call: %v", err)
	}
	req2, _ := http.NewRequest("GET", "https://api.github.com/a", nil)
	if _, err := tr.RoundTrip(req2); err == nil {
		t.Fatal("expected exhausted error")
	} else if !strings.Contains(err.Error(), "exhausted") {
		t.Errorf("err = %v, want exhausted", err)
	}
}

func TestReplayDoneShortfall(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "c.json")
	writeCassette(t, path, &Cassette{
		Interactions: []Interaction{
			{Request: Request{Method: "GET", URL: "https://api.github.com/a"}, Response: Response{Status: 200}},
			{Request: Request{Method: "GET", URL: "https://api.github.com/b"}, Response: Response{Status: 200}},
		},
	})
	tr, err := NewTransport(path, ModeReplay)
	if err != nil {
		t.Fatal(err)
	}
	req, _ := http.NewRequest("GET", "https://api.github.com/a", nil)
	if _, err := tr.RoundTrip(req); err != nil {
		t.Fatalf("first call: %v", err)
	}
	if err := tr.Done(); err == nil {
		t.Fatal("expected unconsumed error")
	} else if !strings.Contains(err.Error(), "unconsumed") {
		t.Errorf("err = %v, want unconsumed", err)
	}
}

func TestReplayBodyMatch(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "c.json")
	writeCassette(t, path, &Cassette{
		Interactions: []Interaction{
			{
				Request: Request{
					Method: "POST",
					URL:    "https://api.github.com/graphql",
					Body:   encodeBody([]byte(`{"query":"{viewer{login}}","variables":{"owner":"octocat"}}`)),
				},
				Response: Response{Status: 200, Body: encodeBody([]byte(`{"data":{}}`))},
			},
		},
	})
	tr, err := NewTransport(path, ModeReplay)
	if err != nil {
		t.Fatal(err)
	}
	// Wrong vars → miss.
	bad, _ := http.NewRequest("POST", "https://api.github.com/graphql",
		strings.NewReader(`{"query":"{viewer{login}}","variables":{"owner":"dependabot"}}`))
	if _, err := tr.RoundTrip(bad); err == nil {
		t.Fatal("expected body mismatch error for different vars")
	}
	// Right vars but with extra whitespace → match.
	good, _ := http.NewRequest("POST", "https://api.github.com/graphql",
		strings.NewReader("{\n\t\"variables\":  {\"owner\":\"octocat\"},\n\t\"query\":\"{viewer{login}}\"\n}"))
	resp, err := tr.RoundTrip(good)
	if err != nil {
		t.Fatalf("expected body match, got %v", err)
	}
	defer resp.Body.Close()
}

func TestReplayMissingCassetteFile(t *testing.T) {
	dir := t.TempDir()
	if _, err := NewTransport(filepath.Join(dir, "absent.json"), ModeReplay); !errors.Is(err, ErrCassetteMissing) {
		t.Fatalf("err = %v, want ErrCassetteMissing", err)
	}
}
