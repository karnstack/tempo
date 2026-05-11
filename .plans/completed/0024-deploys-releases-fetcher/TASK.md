---
id: 0024
slug: deploys-releases-fetcher
title: Deployments + Releases fetcher
status: done
depends_on: [0020]
owner: ""
est_minutes: 120
tags: [github]
autonomy: full
skills: [systematic-debugging]
---

## Goal

Add two REST list fetchers — `internal/github/deployments` (over
`GET /repos/{owner}/{repo}/deployments`) and `internal/github/releases`
(over `GET /repos/{owner}/{repo}/releases`). Each mirrors the
`internal/github/commits` shape exactly: conditional `If-None-Match` ETag
(304 → no rate-limit cost), page-number pagination derived from the `Link`
response header, stateless, no DB access, hermetic VCR-replay tests with
hand-authored cassettes gated behind a `gen` build tag.

These are the bottom layer of the deploys ingest pipeline (task 0030 will
compose them into the unified `deployments` table — `gh_id PK, repo_id,
environment, ref, sha, status, created_at` — per spec line 104). The spec
explicitly sources the `deployments` table from "GitHub Deployments +
Releases", so we expose both shapes here and let 0030 dedup/merge.

Design choices:

- **Two sibling packages, not one.** Matches the project's existing
  one-package-per-endpoint convention (`commits`, `prs`, `prconvo`). The
  packages share no code; small duplication of `hasNextLink` and
  `newReplayClient` is the same tradeoff the existing fetchers already pay,
  and it keeps each fetcher surgical and independently testable.
- **REST + ETag, on purpose.** Same logic as 0023 commits: spec line
  132 calls out conditional REST with ETag because 304s do not consume the
  rate-limit budget. Deploys and releases are low-volume signals; we pay
  the full GET on first-touch and cheap 304s for every poll afterward.
- **No `since=` parameter — ETag is the cursor.** Neither endpoint accepts
  a server-side date filter. We rely entirely on conditional ETag polling
  for "did anything new arrive?". Caller stores `etag` in `sync_cursors`;
  if 304, nothing changed. (Deployments accepts `sha`, `ref`, `environment`
  filters; releases accepts none. We expose the deployment filters since
  they're useful for "deploys to production only" follow-up queries.)
- **Status is out of scope for `deployments`.** The list endpoint does
  NOT return the latest deployment status — that requires a separate
  `GET /repos/{owner}/{repo}/deployments/{id}/statuses` call. Same pattern
  as 0023's commits-additions-deletions scope-out: surgical fetcher, the
  caller composes detail calls if needed. Documented in `doc.go`.
- **Reuse `prs.Author`.** REST returns `{login, id, type}` for `creator`
  (deployments) / `author` (releases) — byte-compatible with
  `prs.Author{GHID, Login, Type}`. Null actor → `prs.Author{Type:"Ghost"}`.
  Local `parseActor` helper per package (don't reach into `prs` for it).
- **Page-number cursors via Link header.** Identical to commits: parse
  `rel="next"` for `HasNext`, expose `NextPage int`. Caller-friendly.
- **ETag only honoured on Page 1.** Each (URL+query) has its own ETag on
  the server; the first page is the one worth caching for the "did
  anything new arrive?" check. The fetcher silently drops the
  caller-supplied ETag when `Page > 1`.

Spec refs:
- `docs/superpowers/specs/2026-05-08-tempo-design.md` data model (line 104):
  `deployments(gh_id PK, repo_id, environment, ref, sha, status,
  created_at)` — sourced from GitHub Deployments + Releases.
- Ingest strategy (lines 130–135): conditional REST with ETag.
- Resources fetched (line 149): "Deployments and Releases."
- Plan row 145: stub, dep 0020, autonomy `full`, skill
  `systematic-debugging`.

## Acceptance criteria

### Both packages

- [ ] Each package has `doc.go` + `fetcher.go` + `fetcher_test.go` +
      `cassettes_gen_test.go` + `testdata/` directory with three cassettes
      (parallel to `internal/github/commits/`).
- [ ] `Fetcher` struct + `New(c *github.Client) *Fetcher` ctor.
- [ ] One method:
      `Fetch(ctx context.Context, owner, repo string, opts FetchOptions) (*Page, error)`.
- [ ] `PerPage` clamped to `[1,100]`; `PerPage<=0` defaults to 50.
- [ ] `Page<=0` becomes 1. `ETag` sent as `If-None-Match` **only** when
      `Page<=1` (silently dropped otherwise).
- [ ] `Page{<Items> [], HasNext, NextPage, NotModified, ETag}`. On 304:
      `NotModified=true`, items empty, `HasNext=false`, `NextPage=0`,
      `ETag` echoes caller's input when server omits.
- [ ] `HasNext` derived from response Link header's `rel="next"`
      (case-insensitive scan via shared `hasNextLink` logic — duplicate
      per package, same pattern commits/prs use).
- [ ] HTTP errors propagate as `*github.HTTPError` (no extra wrapping
      beyond `fmt.Errorf("<pkg>: fetch ...: %w", err)`).
- [ ] Three cassettes per package:
    - `list_page.json` — 200, multiple items with mixed actor types
      (User, Bot, Ghost). Link header `rel="next"`. Server-returned ETag.
    - `list_not_modified.json` — 304, no body, no ETag header (exercises
      caller-echo fallback path).
    - `list_http_error.json` — 404, JSON error body.
- [ ] Every replay test calls `tr.Done()` in `t.Cleanup` so stale
      cassettes fail loud.
- [ ] Cassette-authoring code lives in `//go:build gen`-gated file.
      Re-runnable with `go test -tags=gen -run TestGen_Cassettes
      ./internal/github/{deployments,releases}/...`.

### Package `internal/github/deployments`

- [ ] `Deployment` struct:
      `{GHID int64, SHA, Ref, Task, Environment, Description string,
      Creator prs.Author, CreatedAt, UpdatedAt time.Time}`.
- [ ] `FetchOptions` struct: `SHA, Ref, Environment string, Page, PerPage
      int, ETag string`. (`sha`, `ref`, `environment` are GitHub's
      supported filters; we expose all three. Empty → omit from query.)
- [ ] `Creator` parsing: `null` → `prs.Author{Type:"Ghost"}`; present →
      `{GHID = id, Login = login, Type = type}`.
- [ ] `Page.Deployments []Deployment` field.
- [ ] No `Status` field on `Deployment` — the list endpoint omits it;
      documented in `doc.go` with the per-statuses-endpoint scope-out.

### Package `internal/github/releases`

- [ ] `Release` struct:
      `{GHID int64, TagName, Name string, Draft, Prerelease bool,
      TargetCommitish, Body string, Author prs.Author,
      CreatedAt, PublishedAt time.Time}`. `PublishedAt` is zero when the
      release is a draft (GitHub returns `null`).
- [ ] `FetchOptions` struct: `Page, PerPage int, ETag string`. (Releases
      endpoint has no query-level filters.)
- [ ] `Author` parsing: `null` → `prs.Author{Type:"Ghost"}`; present →
      `{GHID, Login, Type}`.
- [ ] `Page.Releases []Release` field.
- [ ] `PublishedAt` parsing tolerates JSON `null` (use `*time.Time` in
      raw struct, dereference in `toRelease`).

### Verification

- [ ] `go vet ./internal/github/...` clean.
- [ ] `go build ./...` clean.
- [ ] `go test ./internal/github/{deployments,releases}/... -race -count=1`
      passes.
- [ ] `go test ./internal/github/... -race -count=1` passes (no regressions
      in `prs`/`prconvo`/`commits`/`vcr`).
- [ ] `go test -tags=record -run='^$' ./internal/github/{deployments,releases}/...`
      compiles.
- [ ] `go test -tags=gen -run='^$' ./internal/github/{deployments,releases}/...`
      compiles.

## Files to touch

- `internal/github/deployments/doc.go` (new)
- `internal/github/deployments/fetcher.go` (new)
- `internal/github/deployments/fetcher_test.go` (new)
- `internal/github/deployments/cassettes_gen_test.go` (new — `//go:build gen`)
- `internal/github/deployments/testdata/list_page.json` (new — by gen)
- `internal/github/deployments/testdata/list_not_modified.json` (new — by gen)
- `internal/github/deployments/testdata/list_http_error.json` (new — by gen)
- `internal/github/releases/doc.go` (new)
- `internal/github/releases/fetcher.go` (new)
- `internal/github/releases/fetcher_test.go` (new)
- `internal/github/releases/cassettes_gen_test.go` (new — `//go:build gen`)
- `internal/github/releases/testdata/list_page.json` (new — by gen)
- `internal/github/releases/testdata/list_not_modified.json` (new — by gen)
- `internal/github/releases/testdata/list_http_error.json` (new — by gen)
- `.plans/upnext/0024-deploys-releases-fetcher/verify.sh` (overwritten)

## Steps

Each step ends in a small commit. Pattern matches 0023's history.

### 1. Scaffold `internal/github/deployments`

Create `doc.go` modelled on `commits/doc.go`. Cover:
- Purpose (deployments via REST list with ETag cursor).
- Where it sits in the ingest pipeline (0030 will compose; persistence /
  cursor storage are in `internal/ingest`, not here).
- The ETag-as-cursor composition rule (no `since`; deploy/release volumes
  are low, ETag carries the freshness signal alone).
- The explicit scope-out for deployment **statuses** (separate endpoint).

Commit: `chore(github/deployments): scaffold package`

### 2. Deployments — types + fetcher

Write `internal/github/deployments/fetcher.go`. Same skeleton as
`commits/fetcher.go`. Notable differences:

```go
type Deployment struct {
    GHID        int64
    SHA         string
    Ref         string
    Task        string
    Environment string
    Description string
    Creator     prs.Author
    CreatedAt   time.Time
    UpdatedAt   time.Time
}

type FetchOptions struct {
    SHA         string
    Ref         string
    Environment string
    Page        int
    PerPage     int
    ETag        string
}

// Query construction:
//   page, per_page always set
//   sha, ref, environment set only when non-empty
//   No `since` — endpoint doesn't support it.

type rawDeployment struct {
    ID          int64     `json:"id"`
    SHA         string    `json:"sha"`
    Ref         string    `json:"ref"`
    Task        string    `json:"task"`
    Environment string    `json:"environment"`
    Description string    `json:"description"`
    Creator     *rawActor `json:"creator"`
    CreatedAt   time.Time `json:"created_at"`
    UpdatedAt   time.Time `json:"updated_at"`
}
```

Run `go vet ./internal/github/...` and `go build ./...`.

Commit: `feat(github/deployments): types + REST fetcher with ETag/page`

### 3. Deployments — cassettes

Write `internal/github/deployments/cassettes_gen_test.go` (`//go:build gen`).
Three subtests mirroring commits:

- `list_page`: 200 with three deployments — User creator (alice deploying
  to `production`), Bot creator (deploybot to `staging`), null creator
  (Ghost). Each with distinct `sha`/`ref`/`task` values. Link header
  `rel="next"`. Server ETag `W/"dep-abc"`.
- `list_not_modified`: 304, no body, no ETag header.
- `list_http_error`: 404 for `/repos/ghost-org/missing-repo/deployments`.

Helpers in this file: `deploymentsRequestURL(...)` and `mustMarshal`.

Run: `go test -tags=gen -run TestGen_Cassettes ./internal/github/deployments/...`.

Commit: `test(github/deployments): VCR cassettes + gated authoring entrypoint`

### 4. Deployments — replay tests

Write `internal/github/deployments/fetcher_test.go`. Three subtests
mirroring commits: `TestFetch_Page`, `TestFetch_NotModified`,
`TestFetch_HTTPError`. Validate all three creator shapes, ETag passthrough,
HasNext / NextPage, NotModified semantics, and `*github.HTTPError` for 404.

Run: `go test ./internal/github/deployments/... -race -count=1`.

Commit: `test(github/deployments): replay tests for happy/304/HTTPError paths`

### 5. Scaffold `internal/github/releases`

Create `doc.go` modelled on `deployments/doc.go`. Note differences from
deployments: no query filters (releases endpoint accepts none); release
has `draft`/`prerelease` flags and a nullable `published_at`.

Commit: `chore(github/releases): scaffold package`

### 6. Releases — types + fetcher

Write `internal/github/releases/fetcher.go`. Notable bits:

```go
type Release struct {
    GHID            int64
    TagName         string
    Name            string
    Draft           bool
    Prerelease      bool
    TargetCommitish string
    Body            string
    Author          prs.Author
    CreatedAt       time.Time
    PublishedAt     time.Time // zero when Draft and unpublished
}

type FetchOptions struct {
    Page    int
    PerPage int
    ETag    string
}

type rawRelease struct {
    ID              int64      `json:"id"`
    TagName         string     `json:"tag_name"`
    Name            string     `json:"name"`
    Draft           bool       `json:"draft"`
    Prerelease      bool       `json:"prerelease"`
    TargetCommitish string     `json:"target_commitish"`
    Body            string     `json:"body"`
    Author          *rawActor  `json:"author"`
    CreatedAt       time.Time  `json:"created_at"`
    PublishedAt     *time.Time `json:"published_at"` // nullable for drafts
}

func (r rawRelease) toRelease() Release {
    var published time.Time
    if r.PublishedAt != nil {
        published = *r.PublishedAt
    }
    return Release{
        GHID:            r.ID,
        TagName:         r.TagName,
        Name:            r.Name,
        Draft:           r.Draft,
        Prerelease:      r.Prerelease,
        TargetCommitish: r.TargetCommitish,
        Body:            r.Body,
        Author:          parseActor(r.Author),
        CreatedAt:       r.CreatedAt,
        PublishedAt:     published,
    }
}
```

Commit: `feat(github/releases): types + REST fetcher with ETag/page`

### 7. Releases — cassettes

Write `internal/github/releases/cassettes_gen_test.go` (`//go:build gen`).

- `list_page`: 200 with three releases — `v1.0.0` (published, User author,
  not draft, not prerelease), `v1.1.0-rc.1` (published, User author,
  prerelease=true), `v2.0.0-draft` (draft=true, `published_at: null`,
  null author → Ghost). Link header `rel="next"`. Server ETag
  `W/"rel-abc"`.
- `list_not_modified`: 304, no body, no ETag.
- `list_http_error`: 404 for `/repos/ghost-org/missing-repo/releases`.

Run: `go test -tags=gen -run TestGen_Cassettes ./internal/github/releases/...`.

Commit: `test(github/releases): VCR cassettes + gated authoring entrypoint`

### 8. Releases — replay tests

Write `internal/github/releases/fetcher_test.go`. Validate:
- All three author shapes.
- `Draft`/`Prerelease` flags propagate.
- `PublishedAt` is zero for the draft release (exercises the
  `*time.Time` → zero-Time conversion).
- `PublishedAt` is non-zero for published releases.
- ETag passthrough, HasNext, NextPage, NotModified, `*github.HTTPError`
  for 404.

Run: `go test ./internal/github/releases/... -race -count=1`.

Commit: `test(github/releases): replay tests for happy/304/HTTPError paths`

### 9. Verify

Run `./verify.sh` from the task directory. Capture last ~30 lines for
`RESULT.md`.

Commit: (folded into the final task move via `/next-task` autonomy=full
flow — verify.sh is rewritten as part of the in_progress flip and lives
in this task dir.)

## Notes

- The github client's `applyHeaders` adds caller-supplied headers via
  `req.Header.Add` (not Set). `If-None-Match` wants Add semantics. Same
  rationale as 0023.
- Releases' `published_at` is `null` for drafts. Use `*time.Time` in the
  raw struct, dereference in `toRelease`, and surface zero `time.Time` to
  the caller. Tests verify this path explicitly.
- Deployments' `description` can be `null` in the wire format (GitHub
  sometimes returns null vs empty string). `string` zero-value handles
  both — JSON decoder treats `null` as "leave default", which is `""`.
  Worth a comment in `fetcher.go` so a future reader doesn't worry.
- The deployments list endpoint also returns `payload` (sometimes object,
  sometimes string — GitHub's API is genuinely inconsistent here). We
  drop it. If a future ingest task wants it, surface it as
  `json.RawMessage`.
- Do NOT pre-emptively add a `FetchStatuses` method for deployments in
  this task. Same surgical-scope rule as commits' additions/deletions.
  Goes into 0030 or a follow-up.
- The `prs.Author` import creates an upward dep from
  `deployments`/`releases` → `prs`. Same direction `prconvo`/`commits`
  already use; fine.
- ETags on REST are weak (`W/"..."`); GitHub treats `If-None-Match` against
  weak ETags correctly. Pass through verbatim.
