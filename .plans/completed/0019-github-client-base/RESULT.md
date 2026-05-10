# 0019 — github client base: result

## What changed

New package `internal/github` with:

- `internal/github/doc.go` — package comment (renamed from `github.go` via `git mv`).
- `internal/github/errors.go` — `ErrRateLimited` sentinel, `*HTTPError` (`Is` matches `ErrRateLimited` for status 429), `*GraphQLError` with `GraphQLErrorEntry`.
- `internal/github/limiter.go` — `Limiter` (mutex-guarded), `NewLimiter()`, `Wait(ctx)`, `Update(remaining, resetAt)`, `ctxSleep` helper. Floor=200; "unknown" state (`remaining=-1`) → no-op.
- `internal/github/client.go` — `Client` with `New(token, opts...)`, `REST`, `GraphQL`, options (`WithBaseURL`, `WithHTTPClient`, `WithUserAgent`, `WithMaxRetries`, `WithLogger`, `WithLimiterFloor`, `WithBackoff`, `WithClock`, `WithSleep`). Per-bucket REST + GraphQL limiters, ETag passthrough (304 → `Response{Status:304, Body:nil}` no error), retry on 5xx/429/network errors with `Retry-After` honoured, no retry on other 4xx, body re-buffering for retry safety, jittered exponential backoff capped at 8s.
- `internal/github/limiter_test.go` — 6 unit tests including a `-race` exerciser.
- `internal/github/client_test.go` — 16 behavioural tests against `httptest.NewServer` covering every acceptance bullet plus body re-buffering and `WithLimiterFloor(0)` sanity.

## Verify output

```
==> go vet ./internal/github/...
  ok
==> go build ./...
  ok
==> go test ./internal/github/... -race -count=1
ok  	github.com/karnstack/tempo/internal/github	1.414s
  ok
==> go test ./... (no regressions)
ok  	github.com/karnstack/tempo/internal/api ...
ok  	github.com/karnstack/tempo/internal/api/auth ...
ok  	github.com/karnstack/tempo/internal/api/me ...
ok  	github.com/karnstack/tempo/internal/api/tokens ...
ok  	github.com/karnstack/tempo/internal/api/web ...
ok  	github.com/karnstack/tempo/internal/auth ...
ok  	github.com/karnstack/tempo/internal/config ...
ok  	github.com/karnstack/tempo/internal/github 5.716s
ok  	github.com/karnstack/tempo/internal/logger ...
ok  	github.com/karnstack/tempo/internal/secret ...
ok  	github.com/karnstack/tempo/internal/storage/sqlite ...
  ok
VERIFY OK
```

## Followups (out of scope here)

- 0020 will inject an `*http.Client{Transport: vcrRoundTripper}` via `WithHTTPClient` — no Client changes required.
- 0026 will provide `fx.Provide(github.New)` per `gh_tokens` row when the worker scheduler lands. No fx wiring shipped here (no consumer yet).
- One small deviation from the plan: I dropped `,omitempty` on `Variables` in the GraphQL request envelope so callers can verify the field is always present. GitHub accepts `null` variables fine; downstream queries always send a map regardless.
- Defensive fix vs. the plan: `if herr != nil && !retryable` now closes `resp.Body` before returning — the plan code leaked the body on 4xx.
