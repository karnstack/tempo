package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// newTestClient constructs a Client pointed at srv with hermetic defaults
// (zero backoff, no real sleeping) plus any caller-supplied overrides.
func newTestClient(t *testing.T, srv *httptest.Server, opts ...Option) *Client {
	t.Helper()
	all := append([]Option{
		WithBaseURL(srv.URL),
		WithBackoff(func(int) time.Duration { return 0 }),
		WithMaxRetries(3),
	}, opts...)
	return New("test-token", all...)
}

// recorder captures sleep durations passed to c.sleep / limiter.sleep.
type recorder struct {
	mu    sync.Mutex
	calls []time.Duration
}

func (r *recorder) sleep(_ context.Context, d time.Duration) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, d)
	return nil
}

func (r *recorder) snapshot() []time.Duration {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]time.Duration, len(r.calls))
	copy(out, r.calls)
	return out
}

func TestRESTSendsAuthAndUserAgent(t *testing.T) {
	var gotAuth, gotUA, gotAccept string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotUA = r.Header.Get("User-Agent")
		gotAccept = r.Header.Get("Accept")
		w.WriteHeader(200)
	}))
	defer srv.Close()

	c := newTestClient(t, srv, WithUserAgent("tempo-test/1.0"))
	if _, err := c.REST(context.Background(), "GET", "/x", nil, nil); err != nil {
		t.Fatalf("REST: %v", err)
	}
	if gotAuth != "Bearer test-token" {
		t.Errorf("Authorization = %q, want Bearer test-token", gotAuth)
	}
	if gotUA != "tempo-test/1.0" {
		t.Errorf("User-Agent = %q, want tempo-test/1.0", gotUA)
	}
	if gotAccept != "application/vnd.github+json" {
		t.Errorf("Accept = %q, want application/vnd.github+json", gotAccept)
	}
}

func TestGraphQLSendsAuthAndUserAgent(t *testing.T) {
	var gotAuth, gotUA, gotAccept string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotUA = r.Header.Get("User-Agent")
		gotAccept = r.Header.Get("Accept")
		_, _ = w.Write([]byte(`{"data":{}}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	var out struct{}
	if err := c.GraphQL(context.Background(), "{ x }", nil, &out); err != nil {
		t.Fatalf("GraphQL: %v", err)
	}
	if gotAuth != "Bearer test-token" {
		t.Errorf("Authorization = %q", gotAuth)
	}
	if gotUA != DefaultUserAgent {
		t.Errorf("User-Agent = %q, want %q", gotUA, DefaultUserAgent)
	}
	if gotAccept != "application/json" {
		t.Errorf("Accept = %q, want application/json", gotAccept)
	}
}

func TestGraphQLPostShape(t *testing.T) {
	var gotQuery string
	var gotVars map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("method = %s, want POST", r.Method)
		}
		body, _ := io.ReadAll(r.Body)
		var got struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		if err := json.Unmarshal(body, &got); err != nil {
			t.Errorf("unmarshal request: %v raw=%s", err, body)
		}
		gotQuery = got.Query
		gotVars = got.Variables
		_, _ = w.Write([]byte(`{"data":{"hello":"world"}}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	var out struct {
		Hello string `json:"hello"`
	}
	err := c.GraphQL(context.Background(), "query Q { hello }", map[string]any{"foo": "bar"}, &out)
	if err != nil {
		t.Fatalf("GraphQL: %v", err)
	}
	if gotQuery != "query Q { hello }" {
		t.Errorf("query = %q", gotQuery)
	}
	if v, _ := gotVars["foo"].(string); v != "bar" {
		t.Errorf("variables[foo] = %v, want bar", gotVars["foo"])
	}
	if out.Hello != "world" {
		t.Errorf("out.Hello = %q, want world", out.Hello)
	}
}

func TestGraphQLApplicationErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"errors":[{"message":"boom","type":"NOT_FOUND","path":["a",1,"b"]}]}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	out := struct {
		X string `json:"x"`
	}{X: "untouched"}
	err := c.GraphQL(context.Background(), "{ x }", nil, &out)
	var ge *GraphQLError
	if !errors.As(err, &ge) {
		t.Fatalf("err = %v, want *GraphQLError", err)
	}
	if len(ge.Errors) != 1 || ge.Errors[0].Message != "boom" || ge.Errors[0].Type != "NOT_FOUND" {
		t.Errorf("entries = %+v", ge.Errors)
	}
	if out.X != "untouched" {
		t.Errorf("out.X = %q, want untouched", out.X)
	}
	// Path mixes string and int — verify both decoded.
	if len(ge.Errors[0].Path) != 3 {
		t.Fatalf("path len = %d, want 3", len(ge.Errors[0].Path))
	}
	if s, _ := ge.Errors[0].Path[0].(string); s != "a" {
		t.Errorf("path[0] = %v", ge.Errors[0].Path[0])
	}
	if n, _ := ge.Errors[0].Path[1].(float64); n != 1 {
		t.Errorf("path[1] = %v (%T)", ge.Errors[0].Path[1], ge.Errors[0].Path[1])
	}
}

func TestREST304NotModified(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("ETag", `"abc"`)
		w.WriteHeader(304)
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	resp, err := c.REST(context.Background(), "GET", "/x", nil, nil)
	if err != nil {
		t.Fatalf("REST: %v", err)
	}
	if resp.Status != 304 {
		t.Errorf("status = %d, want 304", resp.Status)
	}
	if len(resp.Body) != 0 {
		t.Errorf("body len = %d, want 0", len(resp.Body))
	}
	if resp.ETag != `"abc"` {
		t.Errorf("etag = %q, want %q", resp.ETag, `"abc"`)
	}
}

func TestRESTETagRoundTrip(t *testing.T) {
	var gotIfNoneMatch string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotIfNoneMatch = r.Header.Get("If-None-Match")
		w.WriteHeader(200)
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	headers := http.Header{}
	headers.Set("If-None-Match", `"abc"`)
	if _, err := c.REST(context.Background(), "GET", "/x", nil, headers); err != nil {
		t.Fatalf("REST: %v", err)
	}
	if gotIfNoneMatch != `"abc"` {
		t.Errorf("server saw If-None-Match = %q, want %q", gotIfNoneMatch, `"abc"`)
	}
}

func TestRESTRateLimitPauseBeforeNextCall(t *testing.T) {
	fixedNow := time.Unix(1747000000, 0)
	resetUnix := fixedNow.Add(time.Second).Unix()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-RateLimit-Remaining", "0")
		w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(resetUnix, 10))
		w.WriteHeader(200)
	}))
	defer srv.Close()

	rec := &recorder{}
	c := newTestClient(t, srv,
		WithClock(func() time.Time { return fixedNow }),
		WithSleep(rec.sleep),
	)
	// First call: limiter is fresh, no sleep.
	if _, err := c.REST(context.Background(), "GET", "/x", nil, nil); err != nil {
		t.Fatalf("first REST: %v", err)
	}
	if got := rec.snapshot(); len(got) != 0 {
		t.Fatalf("after first call, sleeps = %v, want none", got)
	}
	// Second call: limiter says remaining=0, should sleep ~1s before issuing.
	if _, err := c.REST(context.Background(), "GET", "/x", nil, nil); err != nil {
		t.Fatalf("second REST: %v", err)
	}
	got := rec.snapshot()
	if len(got) != 1 {
		t.Fatalf("sleeps = %v, want exactly one", got)
	}
	if got[0] != time.Second {
		t.Errorf("sleep = %v, want 1s", got[0])
	}
}

func TestRESTRetriesOn5xxThenSucceeds(t *testing.T) {
	var attempts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := atomic.AddInt32(&attempts, 1)
		if n < 3 {
			w.WriteHeader(503)
			return
		}
		_, _ = w.Write([]byte(`ok`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	resp, err := c.REST(context.Background(), "GET", "/x", nil, nil)
	if err != nil {
		t.Fatalf("REST: %v", err)
	}
	if string(resp.Body) != "ok" {
		t.Errorf("body = %q, want ok", resp.Body)
	}
	if got := atomic.LoadInt32(&attempts); got != 3 {
		t.Errorf("attempts = %d, want 3", got)
	}
}

func TestRESTRetriesExhaustedReturnsHTTPError(t *testing.T) {
	var attempts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&attempts, 1)
		w.WriteHeader(503)
		_, _ = w.Write([]byte(`down`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv) // maxRetries=3 → 4 attempts total
	_, err := c.REST(context.Background(), "GET", "/x", nil, nil)
	var he *HTTPError
	if !errors.As(err, &he) {
		t.Fatalf("err = %v, want *HTTPError", err)
	}
	if he.Status != 503 {
		t.Errorf("status = %d, want 503", he.Status)
	}
	if !strings.Contains(string(he.Body), "down") {
		t.Errorf("body = %q, want contains 'down'", he.Body)
	}
	if got := atomic.LoadInt32(&attempts); got != 4 {
		t.Errorf("attempts = %d, want 4 (1 initial + 3 retries)", got)
	}
}

func TestREST429HonoursRetryAfter(t *testing.T) {
	var attempts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := atomic.AddInt32(&attempts, 1)
		if n == 1 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(429)
			return
		}
		_, _ = w.Write([]byte(`ok`))
	}))
	defer srv.Close()

	rec := &recorder{}
	c := newTestClient(t, srv, WithSleep(rec.sleep))
	if _, err := c.REST(context.Background(), "GET", "/x", nil, nil); err != nil {
		t.Fatalf("REST: %v", err)
	}
	got := rec.snapshot()
	if len(got) != 1 {
		t.Fatalf("sleeps = %v, want exactly one (the inter-attempt wait)", got)
	}
	if got[0] < time.Second {
		t.Errorf("sleep = %v, want >= 1s (Retry-After honoured)", got[0])
	}
}

func TestREST429SentinelMatchesAfterExhaustion(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(429)
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	_, err := c.REST(context.Background(), "GET", "/x", nil, nil)
	if !errors.Is(err, ErrRateLimited) {
		t.Fatalf("err = %v, want errors.Is ErrRateLimited", err)
	}
}

func TestRESTNoRetryOn4xxOtherThan429(t *testing.T) {
	var attempts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&attempts, 1)
		w.WriteHeader(404)
		_, _ = w.Write([]byte(`not found`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	_, err := c.REST(context.Background(), "GET", "/x", nil, nil)
	var he *HTTPError
	if !errors.As(err, &he) {
		t.Fatalf("err = %v, want *HTTPError", err)
	}
	if he.Status != 404 {
		t.Errorf("status = %d, want 404", he.Status)
	}
	if got := atomic.LoadInt32(&attempts); got != 1 {
		t.Errorf("attempts = %d, want 1 (no retries on 404)", got)
	}
}

func TestRESTContextCancelDuringRateLimitWait(t *testing.T) {
	fixedNow := time.Unix(1747000000, 0)
	resetUnix := fixedNow.Add(time.Hour).Unix() // far future so we definitely block

	var attempts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&attempts, 1)
		w.Header().Set("X-RateLimit-Remaining", "0")
		w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(resetUnix, 10))
		w.WriteHeader(200)
	}))
	defer srv.Close()

	blockingSleep := func(ctx context.Context, _ time.Duration) error {
		<-ctx.Done()
		return ctx.Err()
	}
	c := newTestClient(t, srv,
		WithClock(func() time.Time { return fixedNow }),
		WithSleep(blockingSleep),
	)
	// Prime limiter.
	if _, err := c.REST(context.Background(), "GET", "/x", nil, nil); err != nil {
		t.Fatalf("priming REST: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()
	_, err := c.REST(ctx, "GET", "/x", nil, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if got := atomic.LoadInt32(&attempts); got != 1 {
		t.Errorf("attempts = %d, want 1 (cancelled before second request)", got)
	}
}

func TestRESTNetworkErrorIsRetryable(t *testing.T) {
	// Closed server → connection refused on every attempt.
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	srv.Close()

	c := newTestClient(t, srv)
	_, err := c.REST(context.Background(), "GET", "/x", nil, nil)
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "github: do") {
		t.Errorf("err = %v, want to contain 'github: do'", err)
	}
}

func TestRESTBuffersBodyForRetry(t *testing.T) {
	var bodies []string
	var mu sync.Mutex
	var attempts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, string(body))
		mu.Unlock()
		if n := atomic.AddInt32(&attempts, 1); n < 2 {
			w.WriteHeader(503)
			return
		}
		w.WriteHeader(200)
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	if _, err := c.REST(context.Background(), "POST", "/x", strings.NewReader(`hello`), nil); err != nil {
		t.Fatalf("REST: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(bodies) != 2 || bodies[0] != "hello" || bodies[1] != "hello" {
		t.Errorf("bodies = %q, want both 'hello' (retry must re-buffer)", bodies)
	}
}

// Sanity check that LimiterFloor=0 disables blocking entirely (used by some
// tests that don't want any rate-limit interference).
func TestWithLimiterFloorZeroNeverBlocks(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-RateLimit-Remaining", "0")
		w.Header().Set("X-RateLimit-Reset", fmt.Sprintf("%d", time.Now().Add(time.Hour).Unix()))
		w.WriteHeader(200)
	}))
	defer srv.Close()

	rec := &recorder{}
	c := newTestClient(t, srv, WithLimiterFloor(0), WithSleep(rec.sleep))
	for i := 0; i < 3; i++ {
		if _, err := c.REST(context.Background(), "GET", "/x", nil, nil); err != nil {
			t.Fatalf("REST #%d: %v", i, err)
		}
	}
	if got := rec.snapshot(); len(got) != 0 {
		t.Errorf("sleeps = %v, want none with floor=0", got)
	}
}
