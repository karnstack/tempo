package vcr

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
)

// recordedResponseHeaders is the allow-list of response headers persisted to
// cassettes. Anything else (Date, Server, Set-Cookie, X-GitHub-Request-Id,
// trace IDs, etc.) is dropped so cassettes don't churn on every re-record.
var recordedResponseHeaders = []string{
	"Cache-Control",
	"Content-Type",
	"Etag",
	"Last-Modified",
	"Retry-After",
	"X-Ratelimit-Limit",
	"X-Ratelimit-Remaining",
	"X-Ratelimit-Reset",
	"X-Ratelimit-Resource",
	"X-Ratelimit-Used",
}

func init() {
	defaultResponseHeaderFilter = func(h http.Header) http.Header {
		out := http.Header{}
		for _, key := range recordedResponseHeaders {
			if v := h.Values(key); len(v) > 0 {
				out[http.CanonicalHeaderKey(key)] = append([]string(nil), v...)
			}
		}
		return out
	}
}

// record forwards the request via the inner transport, captures the response,
// and appends a sanitised interaction to the in-memory cassette. Authorization
// is redacted; response headers are filtered to the allow-list.
func (t *Transport) recordImpl(req *http.Request) (*http.Response, error) {
	body, err := drainBody(req)
	if err != nil {
		return nil, err
	}
	resp, err := t.inner.RoundTrip(req)
	if err != nil {
		return nil, err
	}
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("vcr: read response body: %w", err)
	}
	_ = resp.Body.Close()
	// Restash response body so the caller can read it as if it came off the wire.
	resp.Body = io.NopCloser(bytes.NewReader(respBody))
	resp.ContentLength = int64(len(respBody))

	reqHeader := cloneHeader(req.Header)
	t.scrub(reqHeader)

	t.cassette.Interactions = append(t.cassette.Interactions, Interaction{
		Request: Request{
			Method:  req.Method,
			URL:     req.URL.String(),
			Headers: reqHeader,
			Body:    encodeBody(body),
		},
		Response: Response{
			Status:  resp.StatusCode,
			Headers: t.filter(resp.Header),
			Body:    encodeBody(respBody),
		},
	})
	t.dirty = true
	return resp, nil
}

// closeImpl flushes the in-memory cassette to disk if anything was recorded.
// Safe to call multiple times; subsequent calls after a successful flush are
// no-ops until the next record.
func (t *Transport) closeImpl() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.dirty {
		return nil
	}
	if err := t.cassette.Save(t.path); err != nil {
		return err
	}
	t.dirty = false
	return nil
}
