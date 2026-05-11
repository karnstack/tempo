package vcr

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

func TestEncodeDecodeBody(t *testing.T) {
	cases := []struct {
		name string
		in   []byte
	}{
		{"nil", nil},
		{"empty", []byte("")},
		{"json object", []byte(`{"foo":"bar","n":1}`)},
		{"json array", []byte(`[1,2,3]`)},
		{"non-json text", []byte("not json: 304\nblah")},
		{"binary-ish", []byte{0x01, 0x02, 0x03, 'a', 'b'}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			enc := encodeBody(tc.in)
			dec := decodeBody(enc)
			if len(tc.in) == 0 {
				if dec != nil {
					t.Errorf("decode of empty input = %q, want nil", dec)
				}
				return
			}
			if string(dec) != string(tc.in) {
				t.Errorf("round-trip mismatch: in=%q out=%q", tc.in, dec)
			}
		})
	}
}

func TestEncodeBodyJSONInline(t *testing.T) {
	in := []byte(`{"a":1}`)
	enc := encodeBody(in)
	// Stored inline means the RawMessage equals the input bytes, not a
	// JSON-encoded string of them.
	if string(enc) != string(in) {
		t.Errorf("expected JSON body stored inline, got %q", enc)
	}
}

func TestEncodeBodyNonJSONFallback(t *testing.T) {
	enc := encodeBody([]byte("hello"))
	var s string
	if err := json.Unmarshal(enc, &s); err != nil {
		t.Fatalf("non-JSON body should be stored as JSON string, unmarshal failed: %v (raw=%q)", err, enc)
	}
	if s != "hello" {
		t.Errorf("decoded string = %q, want %q", s, "hello")
	}
}

func TestLoadCassetteMissing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "absent.json")
	c, err := LoadCassette(path)
	if !errors.Is(err, ErrCassetteMissing) {
		t.Fatalf("err = %v, want ErrCassetteMissing", err)
	}
	if c == nil || c.Version != CassetteVersion {
		t.Fatalf("expected empty cassette with version=%d, got %+v", CassetteVersion, c)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "nested", "c.json")

	orig := &Cassette{
		Interactions: []Interaction{
			{
				Request: Request{
					Method:  "POST",
					URL:     "https://api.github.com/graphql",
					Headers: http.Header{"Content-Type": []string{"application/json"}},
					Body:    encodeBody([]byte(`{"query":"{viewer{login}}"}`)),
				},
				Response: Response{
					Status:  200,
					Headers: http.Header{"X-Ratelimit-Remaining": []string{"4998"}},
					Body:    encodeBody([]byte(`{"data":{"viewer":{"login":"octocat"}}}`)),
				},
			},
			{
				Request: Request{
					Method: "GET",
					URL:    "https://api.github.com/repos/x/y",
				},
				Response: Response{
					Status: 304,
					// no body
				},
			},
		},
	}
	if err := orig.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := LoadCassette(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.Version != CassetteVersion {
		t.Errorf("version = %d, want %d", loaded.Version, CassetteVersion)
	}
	if len(loaded.Interactions) != 2 {
		t.Fatalf("interactions = %d, want 2", len(loaded.Interactions))
	}
	// JSON bodies are re-indented by Save (for human-readable cassettes), so
	// compare semantically rather than byte-equal.
	got0 := decodeBody(loaded.Interactions[0].Response.Body)
	var gotObj, wantObj map[string]any
	if err := json.Unmarshal(got0, &gotObj); err != nil {
		t.Fatalf("interaction 0 body not valid JSON: %v (%q)", err, got0)
	}
	if err := json.Unmarshal([]byte(`{"data":{"viewer":{"login":"octocat"}}}`), &wantObj); err != nil {
		t.Fatal(err)
	}
	gotJSON, _ := json.Marshal(gotObj)
	wantJSON, _ := json.Marshal(wantObj)
	if string(gotJSON) != string(wantJSON) {
		t.Errorf("interaction 0 body = %s, want %s", gotJSON, wantJSON)
	}
	if loaded.Interactions[1].Response.Status != 304 {
		t.Errorf("interaction 1 status = %d, want 304", loaded.Interactions[1].Response.Status)
	}
	if len(decodeBody(loaded.Interactions[1].Response.Body)) != 0 {
		t.Errorf("interaction 1 body should be empty")
	}
}

func TestSaveAtomicTempCleanup(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "c.json")
	c := &Cassette{Interactions: []Interaction{}}
	if err := c.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	// Only c.json should remain — no leftover *.tmp-* files.
	if len(entries) != 1 || entries[0].Name() != "c.json" {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("expected only c.json, got %v", names)
	}
}

func TestLoadCassetteWrongVersion(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "c.json")
	bad := []byte(`{"version":999,"interactions":[]}`)
	if err := os.WriteFile(path, bad, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadCassette(path); err == nil {
		t.Fatal("expected version mismatch error")
	}
}
