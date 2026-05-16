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

func WithHTTPClient(h *http.Client) Option         { return func(c *Client) { c.httpc = h } }
func WithBaseURL(u string) Option                  { return func(c *Client) { c.baseURL = u } }
func WithUserAgent(s string) Option                { return func(c *Client) { c.ua = s } }
func WithLogger(l *zap.Logger) Option              { return func(c *Client) { c.log = l } }
func WithMaxRetries(n int) Option                  { return func(c *Client) { c.maxRetries = n } }
func WithBackoff(f func(int) time.Duration) Option { return func(c *Client) { c.backoff = f } }
func WithClock(f func() time.Time) Option          { return func(c *Client) { c.now = f } }
func WithSleep(f func(context.Context, time.Duration) error) Option {
	return func(c *Client) { c.sleep = f }
}
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
		var reqBody io.Reader
		if bodyBytes != nil {
			reqBody = bytes.NewReader(bodyBytes)
		}
		req, err := http.NewRequestWithContext(ctx, method, url, reqBody)
		if err != nil {
			return nil, fmt.Errorf("github: build request: %w", err)
		}
		c.applyHeaders(req, headers, "application/vnd.github+json")
		resp, err := c.httpc.Do(req)
		retryAfter, retryable, herr := c.classify(resp, err)
		if herr != nil && !retryable {
			if resp != nil {
				_ = resp.Body.Close()
			}
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
		Variables map[string]any `json:"variables"`
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
			if resp != nil {
				_ = resp.Body.Close()
			}
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

// GraphQLRemaining reports the most recent X-RateLimit-Remaining observed on
// a GraphQL response. ok=false until the first GraphQL call has completed.
func (c *Client) GraphQLRemaining() (int, bool) { return c.graphql.Remaining() }

// RESTRemaining reports the most recent X-RateLimit-Remaining observed on a
// REST response. ok=false until the first REST call has completed.
func (c *Client) RESTRemaining() (int, bool) { return c.rest.Remaining() }

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
//
// On retryable=true the caller must close resp.Body itself; on retryable=false
// with herr the caller must close resp.Body before returning the error; on
// retryable=false with herr=nil ownership of resp passes to readResponse /
// decodeGraphQL which defer-close.
func (c *Client) classify(resp *http.Response, err error) (time.Duration, bool, error) {
	if err != nil {
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
	jitter := time.Duration(rand.Int63n(int64(base) / 5)) //nolint:gosec
	if rand.Intn(2) == 0 {                                //nolint:gosec
		return base - jitter
	}
	return base + jitter
}
