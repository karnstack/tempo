package vcr

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
)

// CassetteVersion is the on-disk schema version. Bump if the shape changes in
// a way old cassettes can't be read.
const CassetteVersion = 1

// Cassette is a JSON-on-disk recording of HTTP interactions, replayed
// in order against matching requests.
type Cassette struct {
	Version      int           `json:"version"`
	Interactions []Interaction `json:"interactions"`
}

// Interaction is one request/response pair.
type Interaction struct {
	Request  Request  `json:"request"`
	Response Response `json:"response"`
}

// Request is the recorded request side. Headers are stored only for human
// readability — matching uses method+url+body. Body is json.RawMessage so
// JSON payloads embed inline; non-JSON bodies are stored as a JSON string.
type Request struct {
	Method  string          `json:"method"`
	URL     string          `json:"url"`
	Headers http.Header     `json:"headers,omitempty"`
	Body    json.RawMessage `json:"body,omitempty"`
}

// Response is the recorded response side.
type Response struct {
	Status  int             `json:"status"`
	Headers http.Header     `json:"headers,omitempty"`
	Body    json.RawMessage `json:"body,omitempty"`
}

// ErrCassetteMissing is returned by LoadCassette when the file doesn't exist.
// Callers in ModeRecord / ModeAuto treat this as "start fresh"; ModeReplay
// treats it as fatal.
var ErrCassetteMissing = errors.New("vcr: cassette file does not exist")

// LoadCassette reads and parses a cassette file. A missing file returns
// ErrCassetteMissing wrapped with the path; an empty Cassette is returned in
// that case so callers can append in record mode.
func LoadCassette(path string) (*Cassette, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &Cassette{Version: CassetteVersion}, fmt.Errorf("%w: %s", ErrCassetteMissing, path)
		}
		return nil, fmt.Errorf("vcr: read cassette %s: %w", path, err)
	}
	var c Cassette
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("vcr: parse cassette %s: %w", path, err)
	}
	if c.Version != CassetteVersion {
		return nil, fmt.Errorf("vcr: cassette %s version %d, want %d", path, c.Version, CassetteVersion)
	}
	return &c, nil
}

// Save serialises the cassette to path atomically (temp file + rename),
// creating parent directories as needed. Output is indented JSON with a
// trailing newline so cassettes diff cleanly.
func (c *Cassette) Save(path string) error {
	if c.Version == 0 {
		c.Version = CassetteVersion
	}
	if c.Interactions == nil {
		c.Interactions = []Interaction{}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("vcr: mkdir for %s: %w", path, err)
	}
	buf, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("vcr: marshal cassette: %w", err)
	}
	buf = append(buf, '\n')
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("vcr: create temp: %w", err)
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(buf); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return fmt.Errorf("vcr: write temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("vcr: close temp: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("vcr: rename %s -> %s: %w", tmpName, path, err)
	}
	return nil
}

// encodeBody packs raw response/request bytes into json.RawMessage. JSON
// payloads embed inline (pretty-printable); non-JSON falls back to a JSON
// string so it round-trips through Save/Load without losing bytes. Empty
// input returns nil so the field is omitted.
func encodeBody(raw []byte) json.RawMessage {
	if len(raw) == 0 {
		return nil
	}
	if json.Valid(raw) {
		return json.RawMessage(raw)
	}
	out, _ := json.Marshal(string(raw))
	return out
}

// decodeBody reverses encodeBody. A stored JSON string is unwrapped to its raw
// bytes; anything else is returned as-is. Nil input returns nil.
func decodeBody(body json.RawMessage) []byte {
	if len(body) == 0 {
		return nil
	}
	if body[0] == '"' {
		var s string
		if err := json.Unmarshal(body, &s); err == nil {
			return []byte(s)
		}
	}
	return []byte(body)
}
