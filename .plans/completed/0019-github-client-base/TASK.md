---
id: 0019
slug: github-client-base
title: GitHub client base (REST + GraphQL) + rate limiter
status: done
depends_on: [0013]
owner: ""
est_minutes: 90
tags: [github]
autonomy: full
skills: [systematic-debugging]
---

## Goal

Build the foundational `internal/github` package that every downstream fetcher (0021 PRs, 0022 reviews, 0023 commits, 0024 deploys, 0025 org repos) and the worker scheduler (0026) will use. Concretely: a single `Client` bound to one PAT that exposes `REST` and `GraphQL` methods, transparently honours GitHub's rate-limit headers, retries the right kinds of failures with bounded exponential backoff, and surfaces 304 Not-Modified responses cleanly so callers can hold ETags between polls.

This task does **not** ship any resource-specific queries (PR/commit/review). It does **not** wire the Client into fx (no consumer exists yet — the worker scheduler in 0026 will provide it per-token). It does **not** depend on the VCR fixture system (0020 is the next task and will plug in via an `http.RoundTripper` seam this task exposes).

Spec reference: `docs/superpowers/specs/2026-05-08-tempo-design.md`
- lines 125–139 (rate-limit strategy: GraphQL-first, ETag-conditional REST, backoff when `X-RateLimit-Remaining < 200`),
- line 256 (`internal/github/   # GraphQL + REST client, rate limiter`).

Master-plan row: `docs/superpowers/plans/2026-05-08-tempo-implementation.md` line 140.

## Design decisions

- **One `Client` per PAT.** GitHub's rate-limit buckets are per-token; tracking limits at any other scope races. The ingest worker (0026) will instantiate a `Client` per `gh_tokens` row it iterates. The Client is safe for concurrent use (mutex-guarded limiters), but in practice 0026 polls serially.
- **Two limiters per Client (REST + GraphQL).** GitHub returns `X-RateLimit-Remaining` / `X-RateLimit-Reset` headers on both endpoints, but the buckets are independent (5000 req/hr REST, 5000 points/hr GraphQL). Each method updates its own limiter from response headers. We do **not** parse the GraphQL body's `rateLimit{remaining,resetAt}` field — the headers are sufficient and the body shape varies per query.
- **`Limiter` floor = 200, configurable.** Matches the spec. When `remaining` is below the floor, `Wait` blocks until the bucket resets. Initial state is "unknown" (`remaining = -1`) and `Wait` is a no-op until the first `Update` from a response.
- **Retry policy.** Up to 3 attempts (so 4 total tries), exponential backoff (500ms → 1s → 2s, capped at 8s, with ±20% jitter). Retried statuses: **5xx, 429 (secondary rate-limit), and `net.OpError` / `io.EOF` / `context.DeadlineExceeded` from a single attempt** (but never the outer ctx). 4xx other than 429 is a caller bug — surface immediately. GraphQL application errors (non-empty `errors[]` in a 200 response) are surfaced via `GraphQLError`, **not** retried — the caller decides whether to retry on `RATE_LIMITED`.
- **`Retry-After` honoured.** When 429 carries `Retry-After`, the next backoff is `max(Retry-After, computed-backoff)`.
- **ETag passthrough, not storage.** `REST` accepts a caller-supplied `headers http.Header` (so the caller passes `If-None-Match: <etag>`) and a 304 returns a `*Response` with `Status=304` and empty `Body` — **not** an error. ETag persistence is the per-fetcher's job (it lives in `sync_cursors` from 0011). This keeps the base oblivious to storage.
- **`http.RoundTripper` seam for VCR.** The Client accepts `WithHTTPClient(*http.Client)`. 0020 will inject an `*http.Client{Transport: vcrRoundTripper}`. We do **not** introduce a custom `Transport` interface — the standard `*http.Client` is the seam.
- **No fx provider in this task.** Adding one with no consumer is dead code. 0026 will add `fx.Provide(github.New)` when it needs the Client.
- **No `go-retry` dependency.** Backoff is ~10 lines; pulling in a library for it adds surface for nothing. `sethvargo/go-retry` is already indirect (via goose) but stays indirect.
- **User-Agent.** GitHub requires one. Hard-code `tempo/dev (+https://github.com/karnstack/tempo)` for now; a future build-metadata task can swap the version. Caller can override via `WithUserAgent`.
- **No GraphQL schema introspection / typed query helpers.** `GraphQL(ctx, query, vars, out)` takes a raw query string and a destination pointer for the `data` portion. Each fetcher writes its own query and result struct. Going schema-first (`shurcooL/githubv4`-style) would lock the dependency in for marginal gain at this layer — fetchers can still use that lib downstream if we want.

## Acceptance criteria

- [ ] `internal/github/client.go` exports:
  - `New(token string, opts ...Option) *Client` — defaults: base URL `https://api.github.com`, 30s timeout, UA `tempo/dev (+https://github.com/karnstack/tempo)`, 3 retries, no-op zap logger.
  - `Option`s: `WithBaseURL(string)`, `WithHTTPClient(*http.Client)`, `WithUserAgent(string)`, `WithMaxRetries(int)`, `WithLogger(*zap.Logger)`, `WithLimiterFloor(int)`, `WithBackoff(func(attempt int) time.Duration)`, `WithClock(func() time.Time)`.
  - `(*Client).REST(ctx, method, path string, body io.Reader, headers http.Header) (*Response, error)`.
  - `(*Client).GraphQL(ctx, query string, vars map[string]any, out any) error`.
  - `Response{Status int, Headers http.Header, ETag string, Body []byte}`.
- [ ] `internal/github/errors.go` exports:
  - `ErrRateLimited` (sentinel — surfaced after retries are exhausted on 429).
  - `HTTPError{Status int, Body []byte}` with `Error() string` and `Is(target error) bool` so `errors.Is(err, ErrRateLimited)` works.
  - `GraphQLError{Errors []GraphQLErrorEntry}` with `Error() string`. `GraphQLErrorEntry` has `Message string`, `Type string`, `Path []any` (path can hold strings + ints).
- [ ] `internal/github/limiter.go` exports:
  - `Limiter` struct, `NewLimiter() *Limiter`.
  - `(*Limiter).Wait(ctx context.Context) error` — blocks if `remaining < floor` until `resetAt`; honours ctx cancellation.
  - `(*Limiter).Update(remaining int, resetAt time.Time)`.
  - Internal options on the limiter for floor / clock / sleep are settable from the Client (via the `WithLimiterFloor` / `WithClock` Client options that propagate down).
- [ ] Behavioural tests in `internal/github/client_test.go` against `httptest.NewServer`:
  - **Auth**: `REST` and `GraphQL` set `Authorization: Bearer <token>` and `User-Agent: <ua>`.
  - **GraphQL POST**: body is `{"query":"...","variables":{...}}` JSON; `out` receives the `data` field unmarshalled.
  - **GraphQL errors**: response with non-empty `errors[]` returns `*GraphQLError` with the entries; `out` is left untouched.
  - **REST 304**: server returns `304 Not Modified` → `Response{Status:304, Body:nil}` and **no** error.
  - **REST ETag round-trip**: caller passes `If-None-Match: "abc"` in headers; server echoes back; client sees it on the request.
  - **Rate-limit pause**: server returns `200` with `X-RateLimit-Remaining: 0` and `X-RateLimit-Reset: <unix-now+1s>`. The next call blocks ~1s before issuing. Use the injectable clock + sleep to assert this in <50ms wall time, not 1s — the test should be hermetic.
  - **Retry on 5xx**: server returns `503` twice then `200`. Client returns the `200` body. Backoff is the test's own injected `WithBackoff(func(int) time.Duration { return 0 })` so the test runs instantly.
  - **Retry exhausted**: server always returns `503`. Client returns `*HTTPError{Status:503,...}` after 3 retries. Asserting attempt count via a server-side counter.
  - **429 with Retry-After**: server returns `429` once with `Retry-After: 1` then `200`. Test asserts the injected sleep was called with ≥1s.
  - **No retry on 4xx other than 429**: `404` → returned immediately as `*HTTPError`, server-side counter shows exactly 1 attempt.
  - **Context cancellation during wait**: cancel ctx mid-rate-limit-wait → `Wait` returns `ctx.Err()`, no further request issued.
- [ ] `internal/github/limiter_test.go`:
  - `Wait` is a no-op when `remaining` is unknown (-1).
  - `Wait` is a no-op when `remaining ≥ floor`.
  - `Wait` blocks until `resetAt` when `remaining < floor` (use injected clock + sleep capture, no real sleeping).
  - `Wait` returns `ctx.Err()` when ctx is cancelled while blocked.
  - `Update` is goroutine-safe (table-driven race test under `-race`).
- [ ] `go vet ./internal/github/...`, `go build ./...`, `go test ./internal/github/... -race -count=1` all pass.
- [ ] `verify.sh` exits 0.

## Files to touch

- `internal/github/doc.go` (new — package comment)
- `internal/github/client.go` (new)
- `internal/github/errors.go` (new)
- `internal/github/limiter.go` (new)
- `internal/github/client_test.go` (new)
- `internal/github/limiter_test.go` (new)
- `.plans/upnext/0019-github-client-base/verify.sh` (replace stub)

## Steps

### 1. Sketch the package comment + sentinel errors

`internal/github/doc.go`:

```go
// Package github is tempo's GitHub client. A Client is bound to one PAT and
// exposes REST and GraphQL methods that transparently honour GitHub's rate
// limits, retry transient failures, and pass conditional-request headers
// through unchanged. Resource-specific fetchers (PRs, commits, reviews) live
// in their own packages and call this one.
package github
```

`internal/github/errors.go`:

```go
package github

import (
	"errors"
	"fmt"
	"strings"
)

// ErrRateLimited is returned when retries are exhausted on a 429 (secondary
// rate-limit). Distinct from the per-bucket primary limit which Limiter.Wait
// handles transparently.
var ErrRateLimited = errors.New("github: rate limited")

// HTTPError wraps a non-2xx, non-304 response.
type HTTPError struct {
	Status int
	Body   []byte
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("github: HTTP %d: %s", e.Status, snippet(e.Body))
}

// Is allows `errors.Is(err, ErrRateLimited)` to succeed for 429 responses.
func (e *HTTPError) Is(target error) bool {
	return target == ErrRateLimited && e.Status == 429
}

// GraphQLError is returned by Client.GraphQL when the response carries
// errors[] (regardless of HTTP status — GraphQL signals app errors in 200).
type GraphQLError struct {
	Errors []GraphQLErrorEntry
}

type GraphQLErrorEntry struct {
	Message string `json:"message"`
	Type    string `json:"type,omitempty"`
	Path    []any  `json:"path,omitempty"`
}

func (e *GraphQLError) Error() string {
	msgs := make([]string, 0, len(e.Errors))
	for _, x := range e.Errors {
		msgs = append(msgs, x.Message)
	}
	return "github: graphql: " + strings.Join(msgs, "; ")
}

func snippet(b []byte) string {
	const max = 200
	if len(b) <= max {
		return string(b)
	}
	return string(b[:max]) + "…"
}
```

Commit: `feat(github): sentinel errors for HTTP and graphql failures`

### 2. Build the Limiter

`internal/github/limiter.go`:

```go
package github

import (
	"context"
	"sync"
	"time"
)

// Limiter pauses callers when GitHub says the bucket is nearly empty. State
// is updated from response headers; until the first Update, Wait is a no-op
// (we have no information yet, so we proceed and let the first response
// teach us).
type Limiter struct {
	mu        sync.Mutex
	remaining int
	resetAt   time.Time
	floor     int
	now       func() time.Time
	sleep     func(context.Context, time.Duration) error
}

// NewLimiter returns a Limiter with floor=200, real wall clock, and a
// ctx-aware sleep.
func NewLimiter() *Limiter {
	return &Limiter{
		remaining: -1,
		floor:     200,
		now:       time.Now,
		sleep:     ctxSleep,
	}
}

// Wait blocks if remaining < floor, until the bucket resets or ctx is
// cancelled.
func (l *Limiter) Wait(ctx context.Context) error {
	l.mu.Lock()
	if l.remaining < 0 || l.remaining >= l.floor {
		l.mu.Unlock()
		return nil
	}
	until := l.resetAt
	l.mu.Unlock()
	d := until.Sub(l.now())
	if d <= 0 {
		return nil
	}
	return l.sleep(ctx, d)
}

// Update is called after every API call with the bucket state from response
// headers. Negative remaining is treated as "unknown" and clears state.
func (l *Limiter) Update(remaining int, resetAt time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.remaining = remaining
	l.resetAt = resetAt
}

func ctxSleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
```

Commit: `feat(github): rate-limit aware Limiter`

### 3. Write the Limiter tests

`internal/github/limiter_test.go`. Inject a fake clock + capture sleeps:

```go
package github

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestLimiterWaitNoopWhenUnknown(t *testing.T) {
	l := NewLimiter()
	if err := l.Wait(context.Background()); err != nil {
		t.Fatalf("Wait unknown: %v", err)
	}
}

func TestLimiterWaitNoopWhenAboveFloor(t *testing.T) {
	l := NewLimiter()
	l.Update(500, time.Now().Add(time.Minute))
	if err := l.Wait(context.Background()); err != nil {
		t.Fatalf("Wait above floor: %v", err)
	}
}

func TestLimiterWaitBlocksUntilReset(t *testing.T) {
	now := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
	var slept time.Duration
	l := &Limiter{
		floor: 200,
		now:   func() time.Time { return now },
		sleep: func(_ context.Context, d time.Duration) error { slept = d; return nil },
	}
	l.Update(10, now.Add(2*time.Second))
	if err := l.Wait(context.Background()); err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if slept != 2*time.Second {
		t.Fatalf("slept = %v, want 2s", slept)
	}
}

func TestLimiterWaitCancellable(t *testing.T) {
	l := &Limiter{
		floor: 200,
		now:   time.Now,
		sleep: func(ctx context.Context, _ time.Duration) error { return ctx.Err() },
	}
	l.Update(0, time.Now().Add(time.Hour))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := l.Wait(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Wait err = %v, want Canceled", err)
	}
}

func TestLimiterUpdateRace(t *testing.T) {
	l := NewLimiter()
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(2)
		go func() { defer wg.Done(); l.Update(1, time.Now()) }()
		go func() { defer wg.Done(); _ = l.Wait(context.Background()) }()
	}
	wg.Wait()
}
```

Commit: `test(github): limiter unit tests including race`

### 4. Build the Client

`internal/github/client.go`. The structure:

```go
package github

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"strconv"
	"time"

	"go.uber.org/zap"
)

const (
	DefaultBaseURL   = "https://api.github.com"
	DefaultUserAgent = "tempo/dev (+https://github.com/karnstack/tempo)"
	DefaultFloor     = 200
	DefaultMaxRetry  = 3
)

// Response is the post-rate-limit, post-retry result of a REST call. A 304
// surfaces here with empty Body (not as an error) so callers can use ETags.
type Response struct {
	Status  int
	Headers http.Header
	ETag    string
	Body    []byte
}

type Client struct {
	httpc      *http.Client
	baseURL    string
	token      string
	ua         string
	log        *zap.Logger
	rest       *Limiter
	graphql    *Limiter
	maxRetries int
	backoff    func(attempt int) time.Duration
	now        func() time.Time
	sleep      func(context.Context, time.Duration) error
}

type Option func(*Client)

func WithHTTPClient(h *http.Client) Option           { return func(c *Client) { c.httpc = h } }
func WithBaseURL(u string) Option                    { return func(c *Client) { c.baseURL = u } }
func WithUserAgent(s string) Option                  { return func(c *Client) { c.ua = s } }
func WithLogger(l *zap.Logger) Option                { return func(c *Client) { c.log = l } }
func WithMaxRetries(n int) Option                    { return func(c *Client) { c.maxRetries = n } }
func WithBackoff(f func(int) time.Duration) Option   { return func(c *Client) { c.backoff = f } }
func WithClock(f func() time.Time) Option            { return func(c *Client) { c.now = f } }
func WithLimiterFloor(n int) Option {
	return func(c *Client) { c.rest.floor = n; c.graphql.floor = n }
}

func New(token string, opts ...Option) *Client {
	c := &Client{
		httpc:      &http.Client{Timeout: 30 * time.Second},
		baseURL:    DefaultBaseURL,
		token:      token,
		ua:         DefaultUserAgent,
		log:        zap.NewNop(),
		rest:       NewLimiter(),
		graphql:    NewLimiter(),
		maxRetries: DefaultMaxRetry,
		backoff:    defaultBackoff,
		now:        time.Now,
		sleep:      ctxSleep,
	}
	for _, o := range opts {
		o(c)
	}
	// Propagate clock/sleep into limiters so tests get hermetic behaviour.
	c.rest.now, c.rest.sleep = c.now, c.sleep
	c.graphql.now, c.graphql.sleep = c.now, c.sleep
	return c
}

// REST issues an authenticated request and returns a *Response after
// rate-limit + retry handling. 304s come back as Response{Status:304}, not
// errors. 4xx (other than 429) come back as *HTTPError with no retry. 5xx
// and 429 are retried up to maxRetries with exponential backoff.
func (c *Client) REST(ctx context.Context, method, path string, body io.Reader, headers http.Header) (*Response, error) {
	url := c.baseURL + path
	// Buffer body so retries can re-issue it.
	var bodyBytes []byte
	if body != nil {
		var err error
		bodyBytes, err = io.ReadAll(body)
		if err != nil {
			return nil, fmt.Errorf("github: read body: %w", err)
		}
	}
	for attempt := 0; ; attempt++ {
		if err := c.rest.Wait(ctx); err != nil {
			return nil, err
		}
		req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(bodyBytes))
		if err != nil {
			return nil, fmt.Errorf("github: build request: %w", err)
		}
		c.applyHeaders(req, headers, "application/vnd.github+json")
		resp, err := c.httpc.Do(req)
		retryAfter, retryable, herr := c.classify(resp, err)
		if herr != nil && !retryable {
			return nil, herr
		}
		if resp != nil {
			c.updateLimiter(c.rest, resp.Header)
			if !retryable {
				return c.readResponse(resp)
			}
			_ = resp.Body.Close()
		}
		if attempt >= c.maxRetries {
			if herr != nil {
				return nil, herr
			}
			return nil, errors.New("github: retries exhausted")
		}
		wait := c.backoff(attempt)
		if retryAfter > wait {
			wait = retryAfter
		}
		if err := c.sleep(ctx, wait); err != nil {
			return nil, err
		}
	}
}

// GraphQL POSTs query+vars to /graphql and unmarshals the data field into out.
// Application errors (non-empty errors[]) come back as *GraphQLError. HTTP 5xx
// and 429 are retried; other HTTP errors come back as *HTTPError.
func (c *Client) GraphQL(ctx context.Context, query string, vars map[string]any, out any) error {
	payload, err := json.Marshal(struct {
		Query     string         `json:"query"`
		Variables map[string]any `json:"variables,omitempty"`
	}{query, vars})
	if err != nil {
		return fmt.Errorf("github: marshal graphql: %w", err)
	}
	url := c.baseURL + "/graphql"
	for attempt := 0; ; attempt++ {
		if err := c.graphql.Wait(ctx); err != nil {
			return err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
		if err != nil {
			return fmt.Errorf("github: build graphql request: %w", err)
		}
		c.applyHeaders(req, nil, "application/json")
		resp, err := c.httpc.Do(req)
		retryAfter, retryable, herr := c.classify(resp, err)
		if herr != nil && !retryable {
			return herr
		}
		if resp != nil {
			c.updateLimiter(c.graphql, resp.Header)
			if !retryable {
				return c.decodeGraphQL(resp, out)
			}
			_ = resp.Body.Close()
		}
		if attempt >= c.maxRetries {
			if herr != nil {
				return herr
			}
			return errors.New("github: retries exhausted")
		}
		wait := c.backoff(attempt)
		if retryAfter > wait {
			wait = retryAfter
		}
		if err := c.sleep(ctx, wait); err != nil {
			return err
		}
	}
}

func (c *Client) applyHeaders(req *http.Request, extra http.Header, accept string) {
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("User-Agent", c.ua)
	req.Header.Set("Accept", accept)
	for k, vs := range extra {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}
}

// classify inspects the response/error and decides:
//   - retryAfter: nonzero means honour Retry-After or our backoff, whichever larger
//   - retryable:  true if we should attempt again
//   - herr:       the error to return on the LAST attempt (may be non-nil even when retryable)
func (c *Client) classify(resp *http.Response, err error) (time.Duration, bool, error) {
	if err != nil {
		// Network-layer error: retryable.
		return 0, true, fmt.Errorf("github: do: %w", err)
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 400 {
		return 0, false, nil
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		ra := parseRetryAfter(resp.Header.Get("Retry-After"))
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return ra, true, &HTTPError{Status: resp.StatusCode, Body: body}
	}
	if resp.StatusCode >= 500 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return 0, true, &HTTPError{Status: resp.StatusCode, Body: body}
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	return 0, false, &HTTPError{Status: resp.StatusCode, Body: body}
}

func (c *Client) readResponse(resp *http.Response) (*Response, error) {
	defer resp.Body.Close()
	r := &Response{Status: resp.StatusCode, Headers: resp.Header.Clone(), ETag: resp.Header.Get("ETag")}
	if resp.StatusCode == http.StatusNotModified {
		return r, nil
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("github: read body: %w", err)
	}
	r.Body = body
	return r, nil
}

func (c *Client) decodeGraphQL(resp *http.Response, out any) error {
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("github: read graphql body: %w", err)
	}
	var env struct {
		Data   json.RawMessage     `json:"data"`
		Errors []GraphQLErrorEntry `json:"errors"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return fmt.Errorf("github: unmarshal graphql envelope: %w", err)
	}
	if len(env.Errors) > 0 {
		return &GraphQLError{Errors: env.Errors}
	}
	if out != nil && len(env.Data) > 0 {
		if err := json.Unmarshal(env.Data, out); err != nil {
			return fmt.Errorf("github: unmarshal graphql data: %w", err)
		}
	}
	return nil
}

func (c *Client) updateLimiter(l *Limiter, h http.Header) {
	rem := h.Get("X-RateLimit-Remaining")
	rst := h.Get("X-RateLimit-Reset")
	if rem == "" || rst == "" {
		return
	}
	r, err1 := strconv.Atoi(rem)
	t, err2 := strconv.ParseInt(rst, 10, 64)
	if err1 != nil || err2 != nil {
		return
	}
	l.Update(r, time.Unix(t, 0))
}

func parseRetryAfter(s string) time.Duration {
	if s == "" {
		return 0
	}
	if n, err := strconv.Atoi(s); err == nil {
		return time.Duration(n) * time.Second
	}
	if t, err := http.ParseTime(s); err == nil {
		d := time.Until(t)
		if d < 0 {
			return 0
		}
		return d
	}
	return 0
}

// defaultBackoff: 500ms, 1s, 2s, 4s, 8s — capped at 8s, with ±20% jitter.
func defaultBackoff(attempt int) time.Duration {
	base := 500 * time.Millisecond << attempt
	if base > 8*time.Second {
		base = 8 * time.Second
	}
	jitter := time.Duration(rand.Int63n(int64(base) / 5)) //nolint:gosec // not security-critical
	if rand.Intn(2) == 0 {                                //nolint:gosec
		return base - jitter
	}
	return base + jitter
}
```

Commit: `feat(github): REST + GraphQL client with retry and rate-limit handling`

### 5. Write the Client tests

`internal/github/client_test.go`. Use `httptest.NewServer` and `WithBaseURL(server.URL)`. Each test gets its own server. Helpers to install:

```go
func newClient(t *testing.T, srv *httptest.Server, opts ...Option) *Client {
	t.Helper()
	allOpts := append([]Option{
		WithBaseURL(srv.URL),
		WithBackoff(func(int) time.Duration { return 0 }),
		WithMaxRetries(3),
	}, opts...)
	return New("test-token", allOpts...)
}
```

Cover all bullets in the Acceptance criteria. For the rate-limit pause test, inject a fake `WithClock` and a sleep capture via a custom `WithBackoff` and a manual sleep recorder — easiest is to expose a captured-sleep variant of `WithClock` that just records the requested duration without blocking, so the assertion is "the limiter would have slept ~1s" rather than actually sleeping.

For the 429-retry-after test, capture `c.sleep` calls (you can do this by setting a custom sleep through a small option or by reaching into the unexported field via a helper — easiest: add a `WithSleep(func(context.Context, time.Duration) error)` option in client.go for tests; same machinery as WithBackoff but covering the post-Wait sleep).

Pattern for a counter-server:

```go
var attempts int32
srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	n := atomic.AddInt32(&attempts, 1)
	if n < 3 {
		w.WriteHeader(503)
		return
	}
	_, _ = w.Write([]byte(`ok`))
}))
defer srv.Close()
```

Commit: `test(github): client REST + GraphQL behaviour against httptest`

### 6. Add WithSleep option

In `client.go`, add the test-only-but-not-test-tagged option (mirroring `WithBackoff`):

```go
func WithSleep(f func(context.Context, time.Duration) error) Option {
	return func(c *Client) { c.sleep = f }
}
```

Then re-propagate to limiters at the bottom of `New` (already in place — clock/sleep propagate after options).

Commit: `feat(github): WithSleep option for hermetic tests`

(If you keep this in the same commit as WithBackoff/WithClock, that's fine — group with step 4 instead.)

### 7. Replace verify.sh

```bash
#!/usr/bin/env bash
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"

echo "==> go vet ./internal/github/..."
go vet ./internal/github/...
echo "  ok"

echo "==> go build ./..."
go build ./...
echo "  ok"

echo "==> go test ./internal/github/... -race -count=1"
go test ./internal/github/... -race -count=1
echo "  ok"

echo "==> go test ./... (no regressions)"
go test ./... -race -count=1
echo "  ok"

echo "VERIFY OK"
```

### 8. Run verify

```
./.plans/upnext/0019-github-client-base/verify.sh
```

## Notes

- The `bytes.NewReader(bodyBytes)` re-buffer pattern matters for retry. `http.NewRequestWithContext` consumes the reader once; if we reused the original, the second attempt would send an empty body. Buffering once per call is fine for the request sizes GitHub accepts.
- `applyHeaders` lets the caller override Accept and add If-None-Match/etc. but always wins on Auth + UA — those should not be configurable per-call.
- We deliberately read up to 4 KiB of error bodies. GitHub's error responses are short JSON; truncating prevents an unbounded tail from filling logs if a proxy returns HTML.
- `defaultBackoff` uses `math/rand`; `nolint:gosec` is appropriate — jitter doesn't need crypto.
- GraphQL responses with `errors[]` AND `data` (partial success) are treated as full failure here. That's the safe default; downstream fetchers can re-parse if they ever need partial data.
- The `Limiter`'s `floor` defaults to 200 (matches spec) but is per-instance so a test can drop it to 0 to disable.
- Future task: the worker scheduler (0026) will create one `Client` per `gh_tokens` row and should set `WithLogger(l.With(zap.String("token_id", ...))` so logs identify which bucket.
