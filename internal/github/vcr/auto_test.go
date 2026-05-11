package vcr

import (
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func TestModeAutoMissingFileRecords(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	defer srv.Close()

	dir := t.TempDir()
	path := filepath.Join(dir, "auto-rec.json")
	tr, err := NewTransport(path, ModeAuto)
	if err != nil {
		t.Fatal(err)
	}
	if tr.mode != ModeRecord {
		t.Fatalf("ModeAuto with missing file should promote to ModeRecord, got %v", tr.mode)
	}
	req, _ := http.NewRequest("GET", srv.URL+"/x", nil)
	resp, err := tr.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err := tr.Close(); err != nil {
		t.Fatal(err)
	}
	c, err := LoadCassette(path)
	if err != nil {
		t.Fatalf("cassette not written: %v", err)
	}
	if len(c.Interactions) != 1 {
		t.Errorf("interactions = %d, want 1", len(c.Interactions))
	}
}

func TestModeAutoExistingFileReplays(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "auto-rep.json")
	writeCassette(t, path, &Cassette{
		Interactions: []Interaction{
			{
				Request:  Request{Method: "GET", URL: "https://api.github.com/x"},
				Response: Response{Status: 200, Body: encodeBody([]byte(`{"replayed":true}`))},
			},
		},
	})

	tr, err := NewTransport(path, ModeAuto)
	if err != nil {
		t.Fatal(err)
	}
	if tr.mode != ModeReplay {
		t.Fatalf("ModeAuto with existing file should stay ModeReplay, got %v", tr.mode)
	}
	req, _ := http.NewRequest("GET", "https://api.github.com/x", nil)
	resp, err := tr.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if string(body) != `{"replayed":true}` {
		t.Errorf("body = %s, want replayed", body)
	}
}
