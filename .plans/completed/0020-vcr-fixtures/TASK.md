---
id: 0020
slug: vcr-fixtures
title: VCR-style fixture recorder/replayer for tests
status: done
depends_on: [0019]
owner: ""
est_minutes: 30
tags: [github, test]
autonomy: full
skills: [systematic-debugging]
---

## Goal

Land a tiny, dependency-free VCR layer (`internal/github/vcr`) so fetcher tests
(0021–0024) can exercise the GitHub client against recorded GraphQL/REST
responses instead of burning rate limit. A test wires it in as
`http.RoundTripper`; the transport replays from a JSON cassette by default and
re-records against real GitHub when the `record` build tag is set.

The layer must integrate with the existing `*github.Client` via
`WithHTTPClient(&http.Client{Transport: vcr.NewTransport(...)})` — no client
changes required.

## Acceptance criteria

- [ ] `internal/github/vcr` package compiles using stdlib only (no new go.mod deps).
- [ ] `vcr.NewTransport(cassettePath string, mode Mode, opts ...Option) http.RoundTripper` exists.
- [ ] Three modes: `ModeReplay` (default), `ModeRecord`, `ModeAuto` (replay if file exists, else record).
- [ ] Cassette is JSON-on-disk; bodies stored as `json.RawMessage` (pretty-printable when JSON, fall back to JSON-encoded string when not).
- [ ] Ordered, single-shot matching: each cassette interaction is consumed exactly once, in order. Replay miss / extra interactions → error from `RoundTrip`.
- [ ] Match key: method + URL path + canonicalised query + canonicalised body. GraphQL bodies canonicalised via `json.Unmarshal`+remarshal so whitespace differences don't break matches.
- [ ] On record, `Authorization` request header is redacted to `Bearer REDACTED` before write.
- [ ] On record, response headers persisted are limited to a stable allow-list (`ETag`, `Last-Modified`, `Cache-Control`, `Content-Type`, `Retry-After`, `X-RateLimit-Remaining`, `X-RateLimit-Reset`, `X-RateLimit-Limit`, `X-RateLimit-Used`, `X-RateLimit-Resource`) so cassettes don't churn.
- [ ] Cassette file path is created (with parent dirs) on record; pretty-printed JSON; deterministic field order.
- [ ] Unit tests cover: replay hit, replay miss, ordered consumption, GraphQL canonicalisation, header scrubbing on record, record-then-replay round-trip via in-process upstream.
- [ ] One test file under `//go:build record` demonstrates the record-mode entry point (compile-only — guarded so default `go test` doesn't run it).
- [ ] `internal/github/vcr/doc.go` explains: how to wire it, how to re-record, where cassettes live, what's scrubbed.
- [ ] Existing `internal/github` tests still pass (no regression).
- [ ] `./verify.sh` exits 0.

## Files to touch

- `internal/github/vcr/doc.go` — package doc + usage.
- `internal/github/vcr/cassette.go` — `Cassette`, `Interaction`, `Request`, `Response` types; load/save; matching.
- `internal/github/vcr/mode.go` — `Mode` enum + `ModeFromEnv` helper.
- `internal/github/vcr/transport.go` — `Transport` (impl `http.RoundTripper`).
- `internal/github/vcr/cassette_test.go` — load/save, match canonicalisation, scrubbing.
- `internal/github/vcr/transport_test.go` — replay hit/miss, record-and-replay round trip via local `httptest.Server` upstream.
- `internal/github/vcr/record_demo_test.go` — `//go:build record`, smoke entry-point that re-records against a local upstream (NOT real GitHub — keeps CI hermetic; documents the pattern).
- `.plans/upnext/0020-vcr-fixtures/verify.sh` — `go test ./internal/github/vcr/... ./internal/github/... -count=1 -race`.

## Design notes

### Cassette JSON shape

```json
{
  "version": 1,
  "interactions": [
    {
      "request": {
        "method": "POST",
        "url": "https://api.github.com/graphql",
        "headers": { "Content-Type": ["application/json"] },
        "body": {"query": "...", "variables": {"owner": "octocat"}}
      },
      "response": {
        "status": 200,
        "headers": {
          "Content-Type": ["application/json; charset=utf-8"],
          "X-RateLimit-Remaining": ["4998"],
          "X-RateLimit-Reset":     ["1700000000"]
        },
        "body": {"data": {"viewer": {"login": "octocat"}}}
      }
    }
  ]
}
```

`body` is stored as `json.RawMessage`. On record:

```go
if json.Valid(raw) {
    body = json.RawMessage(raw)              // pretty-prints inline
} else if len(raw) == 0 {
    body = nil
} else {
    body, _ = json.Marshal(string(raw))      // JSON-string fallback for non-JSON bodies
}
```

On replay, the inverse: if the raw bytes start with `"` and unmarshal as a Go
string, hand back the unwrapped bytes; otherwise return as-is.

### Match canonicalisation

```go
func canonicalBody(b []byte) string {
    if len(b) == 0 { return "" }
    var v any
    if err := json.Unmarshal(b, &v); err != nil { return string(b) }
    out, _ := json.Marshal(v)
    return string(out)
}

func canonicalQuery(q url.Values) string {
    keys := slices.Sorted(maps.Keys(q))
    var b strings.Builder
    for i, k := range keys {
        for j, v := range q[k] {
            if i > 0 || j > 0 { b.WriteByte('&') }
            b.WriteString(url.QueryEscape(k))
            b.WriteByte('=')
            b.WriteString(url.QueryEscape(v))
        }
    }
    return b.String()
}
```

Match key = `method + " " + path + "?" + canonicalQuery + "\n" + canonicalBody`.

### Modes

```go
type Mode int
const (
    ModeReplay Mode = iota
    ModeRecord
    ModeAuto    // replay if file exists, else record
)

func ModeFromEnv(def Mode) Mode {
    switch os.Getenv("TEMPO_VCR") {
    case "record": return ModeRecord
    case "replay": return ModeReplay
    case "auto":   return ModeAuto
    default:       return def
    }
}
```

The `record` build tag is the documented re-record entry point; it just sets
`ModeRecord` in the demo test. Production tests pin `ModeReplay` and fail
loud on a miss.

### Transport contract

`RoundTrip` is the only public method. Modes:
- `ModeReplay`: pop next interaction, ensure match, return synthesised `*http.Response`. No-match or no-more-interactions → error wrapping the offending request signature.
- `ModeRecord`: forward via `http.DefaultTransport` (or injected inner via `WithInnerTransport`), capture response, append redacted+filtered interaction, write file.
- `ModeAuto`: stat the cassette path. Exists → replay path. Missing → record path; on `Close`, the new cassette is flushed.

`Close() error` flushes pending writes (record mode). Add `Done() error` to assert all interactions were consumed in replay mode (called from `t.Cleanup`).

## Steps

1. **Scaffold the package and doc.**
   - Create `internal/github/vcr/doc.go` with the package overview.
   - Create empty `mode.go`, `cassette.go`, `transport.go` so `go vet` is happy.
   - Commit: `feat(github/vcr): scaffold package skeleton`.

2. **Mode + env helper.**
   - Implement `Mode`, `String()`, `ModeFromEnv`.
   - Tiny test: env unset → default; `TEMPO_VCR=record` → `ModeRecord`; etc.
   - Commit: `feat(github/vcr): mode enum and TEMPO_VCR env helper`.

3. **Cassette types + load/save.**
   - Define `Cassette`, `Interaction`, `Request`, `Response` with JSON tags.
   - `LoadCassette(path)` — read & unmarshal; missing file → empty cassette + sentinel `errCassetteMissing`.
   - `(*Cassette) Save(path)` — `os.MkdirAll` parent, `json.MarshalIndent` 2-space, atomic via `os.Rename` of temp file.
   - Tests: round-trip a known cassette through Save/Load, body-as-JSON and body-as-string both survive.
   - Commit: `feat(github/vcr): cassette load/save with JSON body fallback`.

4. **Match key + canonicalisation.**
   - Implement `canonicalBody`, `canonicalQuery`, `matchKey`.
   - Tests: identical GraphQL queries with different whitespace match; different `variables` don't match; different query params match regardless of order.
   - Commit: `feat(github/vcr): canonical match keys for replay`.

5. **Transport — replay path.**
   - `Transport` struct: `mode Mode`, `path string`, `cassette *Cassette`, `cursor int`, `inner http.RoundTripper`, `mu sync.Mutex`, opts.
   - `NewTransport(path, mode, opts...)` loads cassette eagerly when mode requires it.
   - `RoundTrip` (replay): lock, peek `cassette.Interactions[cursor]`, compare keys, on match build `*http.Response` from saved bits, advance cursor, return. On mismatch return descriptive error including expected & actual signatures.
   - `Done()`: error if `cursor != len(interactions)`.
   - Tests: hit, miss-different-method, miss-extra-call, miss-shortfall via `Done()`.
   - Commit: `feat(github/vcr): replay transport with ordered match`.

6. **Transport — record path.**
   - `RoundTrip` (record): clone request, drain & restash body, call inner, drain response body, build sanitised `Interaction` (redact `Authorization`, filter response headers via allow-list), append to in-memory cassette.
   - `Close()`: flush via `Save`.
   - Tests: spin a local `httptest.Server`, record one REST + one GraphQL call, close, then load cassette + replay through a fresh transport against `httptest.NewServer` returning 500 (proves replay didn't hit network) — assert bodies + statuses match. Verify `Authorization` did not leak; verify only allow-listed response headers persisted.
   - Commit: `feat(github/vcr): record transport with header scrubbing`.

7. **`ModeAuto` glue + record-tag demo.**
   - Implement `ModeAuto` branch: stat path; missing → record; present → replay.
   - Add `record_demo_test.go` (`//go:build record`) showing `vcr.NewTransport("testdata/demo.json", vcr.ModeRecord)` against a local fake; ensures the build tag plumbing compiles.
   - Tests: auto with missing path records; auto with present path replays.
   - Commit: `feat(github/vcr): auto mode and record-tag demo test`.

8. **Doc + verify.**
   - Flesh out `doc.go`: usage example wiring `vcr.NewTransport` into `github.New(WithHTTPClient(...))`, how to re-record (`go test -tags=record ./...`), what is/isn't redacted, where cassettes live (`testdata/` per fetcher package).
   - Update `.plans/upnext/0020-vcr-fixtures/verify.sh` to run the new tests + the existing `internal/github` tests with `-race -count=1`.
   - Commit: `docs(github/vcr): package usage and re-record workflow`.

9. **Final verification.**
   - Run `./verify.sh`, capture last ~30 lines for `RESULT.md`.
   - If green: write `RESULT.md`, set `status: done`, `git mv` to completed, final commit `feat(github): VCR-style fixture recorder/replayer for tests (#0020)`.

## Notes

- Stdlib only — adding `go-vcr` would carry transitive deps and overshoot the
  hand-rolled approach the spec calls out as fine.
- Cassettes for actual fetchers (PRs, reviews, commits, deploys) are written by
  tasks 0021–0024, not here. This task only ships the machinery and a
  self-test cassette under `testdata/`.
- Don't change `internal/github` itself — VCR slots in via the existing
  `WithHTTPClient` Option.
- Ordered single-shot matching is intentional: it makes test failures point at
  exactly which call diverged from the recorded sequence (cursor + diff). If a
  fetcher needs unordered matching later, we add it as an Option then, not now.
- `Authorization` is the only header we strictly *must* scrub. We don't try to
  scrub repository names or PR bodies — those are intentionally faithful.
