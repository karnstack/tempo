# 0020 — VCR-style fixture recorder/replayer for tests — RESULT

## Summary

Landed `internal/github/vcr` — a stdlib-only `http.RoundTripper` with
replay/record/auto modes, designed to slot into the existing GitHub client
via `WithHTTPClient(&http.Client{Transport: vcr.NewTransport(...)})` with
zero changes to `internal/github`.

Fetcher tasks 0021–0024 can now author cassettes under
`internal/github/<fetcher>/testdata/*.json` and run hermetically in CI.

## Files added

- `internal/github/vcr/doc.go` — usage, re-record workflow, scrub policy.
- `internal/github/vcr/mode.go` — `Mode` enum + `ModeFromEnv` (TEMPO_VCR).
- `internal/github/vcr/cassette.go` — `Cassette`, `Interaction`,
  `Request`, `Response`, atomic Save, body codec + match canonicalisation.
- `internal/github/vcr/transport.go` — `Transport` (http.RoundTripper),
  replay path, `Done()`, `Close()`.
- `internal/github/vcr/transport_record.go` — record path + response-header
  allow-list (init-installed).
- Tests: `mode_test.go`, `cassette_test.go`, `match_test.go`,
  `transport_test.go`, `record_test.go`, `auto_test.go`.
- `internal/github/vcr/record_demo_test.go` — `//go:build record` template
  for fetcher re-records.

## Design highlights

- **Host-agnostic match key.** Cassettes recorded against `api.github.com`
  replay against any local URL a test passes the client.
- **GraphQL-tolerant matching.** Bodies canonicalised through
  Unmarshal/Marshal so whitespace and JSON key order don't break replay.
- **Pretty cassettes, wire-faithful replay.** Save indents JSON bodies for
  diff-readability; Load re-compacts them so replay returns byte-equal
  output to what the upstream sent.
- **Ordered, single-shot interactions.** `Done()` asserts every recorded
  interaction was consumed — stale cassettes fail loud in `t.Cleanup`.
- **Scrub policy.** Authorization redacted; response headers filtered to
  `Cache-Control`, `Content-Type`, `ETag`, `Last-Modified`, `Retry-After`,
  `X-RateLimit-*`. Bodies intentionally faithful.

## Verify output (last lines)

```
==> go vet ./internal/github/...
==> go build ./internal/github/...
==> go test ./internal/github/vcr/... -count=1 -race
ok  	github.com/karnstack/tempo/internal/github/vcr	1.340s
==> go test ./internal/github/... -count=1 -race
ok  	github.com/karnstack/tempo/internal/github	1.641s
ok  	github.com/karnstack/tempo/internal/github/vcr	1.350s
==> compile check: -tags=record
OK
```

## Followups

- 0021–0024 will consume this layer for real fetcher cassettes.
- If a fetcher ever needs unordered matching (e.g. parallel pages
  interleaved), add it as an Option then, not now.
