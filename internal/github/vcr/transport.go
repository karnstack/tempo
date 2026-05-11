package vcr

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
)


// Transport is an http.RoundTripper that records or replays HTTP traffic via a
// JSON cassette on disk. Wire it into the GitHub client with
// github.WithHTTPClient(&http.Client{Transport: tr}).
//
// Replay mode is ordered and single-shot: each interaction matches at most
// once, in the order recorded. A request that doesn't match the next
// interaction (or that arrives after the cassette is exhausted) returns an
// error from RoundTrip. Test code can call Done() in t.Cleanup to assert all
// interactions were consumed — a stale cassette catches drift early.
type Transport struct {
	mode   Mode
	path   string
	inner  http.RoundTripper
	scrub  func(http.Header)
	filter func(http.Header) http.Header

	mu       sync.Mutex
	cassette *Cassette
	cursor   int
	dirty    bool // true when record mode has appended at least one interaction
}

// Option configures a Transport at construction.
type Option func(*Transport)

// WithInnerTransport overrides the http.RoundTripper used in record mode.
// Defaults to http.DefaultTransport.
func WithInnerTransport(rt http.RoundTripper) Option {
	return func(t *Transport) { t.inner = rt }
}

// WithRequestScrubber overrides the function used to redact recorded request
// headers. The default scrubber redacts Authorization to "Bearer REDACTED".
func WithRequestScrubber(fn func(http.Header)) Option {
	return func(t *Transport) { t.scrub = fn }
}

// WithResponseHeaderFilter overrides the function used to whittle recorded
// response headers down to a stable allow-list (defaultResponseHeaderFilter).
func WithResponseHeaderFilter(fn func(http.Header) http.Header) Option {
	return func(t *Transport) { t.filter = fn }
}

// NewTransport constructs a Transport. In ModeReplay or ModeAuto the cassette
// at path is loaded eagerly; a missing file in ModeReplay is an error. In
// ModeAuto a missing file is fine — the transport will record into it.
func NewTransport(path string, mode Mode, opts ...Option) (*Transport, error) {
	t := &Transport{
		mode:   mode,
		path:   path,
		inner:  http.DefaultTransport,
		scrub:  defaultRequestScrubber,
		filter: defaultResponseHeaderFilter,
	}
	for _, o := range opts {
		o(t)
	}
	switch mode {
	case ModeReplay:
		c, err := LoadCassette(path)
		if err != nil {
			return nil, err
		}
		t.cassette = c
	case ModeRecord:
		t.cassette = &Cassette{Version: CassetteVersion}
	case ModeAuto:
		c, err := LoadCassette(path)
		if err != nil && !errors.Is(err, ErrCassetteMissing) {
			return nil, err
		}
		if errors.Is(err, ErrCassetteMissing) {
			// Missing → record so the first run populates the cassette.
			t.mode = ModeRecord
		} else {
			// Present → replay. Resolving at construction means RoundTrip's
			// switch never sees ModeAuto and can dispatch directly.
			t.mode = ModeReplay
		}
		t.cassette = c
	default:
		return nil, fmt.Errorf("vcr: unknown mode %v", mode)
	}
	return t, nil
}

// RoundTrip dispatches to replay or record based on the transport's mode.
func (t *Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	switch t.mode {
	case ModeReplay:
		return t.replay(req)
	case ModeRecord:
		return t.record(req)
	default:
		return nil, fmt.Errorf("vcr: invalid runtime mode %v", t.mode)
	}
}

func (t *Transport) replay(req *http.Request) (*http.Response, error) {
	body, err := drainBody(req)
	if err != nil {
		return nil, err
	}
	wantKey, err := matchKey(req.Method, req.URL.String(), body)
	if err != nil {
		return nil, err
	}
	if t.cursor >= len(t.cassette.Interactions) {
		return nil, fmt.Errorf("vcr: cassette %s exhausted at cursor %d; extra call: %s", t.path, t.cursor, wantKey)
	}
	rec := t.cassette.Interactions[t.cursor]
	gotKey, err := matchKey(rec.Request.Method, rec.Request.URL, decodeBody(rec.Request.Body))
	if err != nil {
		return nil, fmt.Errorf("vcr: cassette %s interaction %d has invalid recorded request: %w", t.path, t.cursor, err)
	}
	if gotKey != wantKey {
		return nil, fmt.Errorf("vcr: cassette %s interaction %d mismatch\n  expected: %s\n  got     : %s",
			t.path, t.cursor, gotKey, wantKey)
	}
	t.cursor++
	return buildResponse(req, rec.Response), nil
}

// Done returns an error if any recorded interactions are unconsumed (replay
// mode) — useful to call from t.Cleanup to catch a shrinking call set. It's a
// no-op in record mode.
func (t *Transport) Done() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.mode != ModeReplay {
		return nil
	}
	if t.cursor < len(t.cassette.Interactions) {
		return fmt.Errorf("vcr: cassette %s has %d unconsumed interaction(s); test only made %d call(s)",
			t.path, len(t.cassette.Interactions)-t.cursor, t.cursor)
	}
	return nil
}

// drainBody reads and re-stashes req.Body so the caller can keep using it.
// Returns nil for a nil body.
func drainBody(req *http.Request) ([]byte, error) {
	if req.Body == nil || req.Body == http.NoBody {
		return nil, nil
	}
	b, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, fmt.Errorf("vcr: read request body: %w", err)
	}
	_ = req.Body.Close()
	req.Body = io.NopCloser(bytes.NewReader(b))
	return b, nil
}

// buildResponse synthesises an http.Response from a recorded Response. The
// returned *http.Response shares ownership rules with normal RoundTrip results
// — the caller must close Body.
func buildResponse(req *http.Request, rec Response) *http.Response {
	body := decodeBody(rec.Body)
	resp := &http.Response{
		StatusCode:    rec.Status,
		Status:        fmt.Sprintf("%d %s", rec.Status, http.StatusText(rec.Status)),
		Proto:         "HTTP/1.1",
		ProtoMajor:    1,
		ProtoMinor:    1,
		Header:        cloneHeader(rec.Headers),
		Body:          io.NopCloser(bytes.NewReader(body)),
		ContentLength: int64(len(body)),
		Request:       req,
	}
	if resp.Header == nil {
		resp.Header = http.Header{}
	}
	return resp
}

func cloneHeader(h http.Header) http.Header {
	if h == nil {
		return http.Header{}
	}
	out := make(http.Header, len(h))
	for k, vs := range h {
		out[k] = append([]string(nil), vs...)
	}
	return out
}

// record forwards via the inner transport and appends a sanitised interaction
// to the in-memory cassette. Flushed to disk by Close.
func (t *Transport) record(req *http.Request) (*http.Response, error) {
	return t.recordImpl(req)
}

// Close flushes recorded interactions to the cassette path. Always safe to
// call (no-op in replay mode or when nothing was recorded). Tests typically
// register it via t.Cleanup.
func (t *Transport) Close() error { return t.closeImpl() }

// defaultRequestScrubber is the no-config scrub used when WithRequestScrubber
// isn't supplied. defaultResponseHeaderFilter is initialised in
// transport_record.go to the production allow-list (passthrough by default
// here so the package compiles without that file's init).
var (
	defaultRequestScrubber      = func(h http.Header) { h.Set("Authorization", "Bearer REDACTED") }
	defaultResponseHeaderFilter = func(h http.Header) http.Header { return h }
)
