---
id: 0041
slug: api-repos-metrics
title: /api/v1/repos + per-repo metrics
status: done
depends_on: [0033, 0034, 0035, 0036]
owner: ""
est_minutes: 75
tags: [api]
autonomy: full
skills: []
---

## Goal

Add the two repo endpoints from
`docs/superpowers/specs/2026-05-08-tempo-design.md:221-222`:

```
GET /api/v1/repos
GET /api/v1/repos/:owner/:name/metrics?from=&to=
```

Both are read-only, tenant-scoped, and hit the daily-rollup tables
populated by 0033–0036. The first lists every repo in the caller's
tenant for the dashboard's repo selector. The second returns the
metrics-time-series the per-repo dashboard wants (lead time,
deploys, review latency, reviewer load).

### Authentication / authorisation

- Both routes mount behind `web.RequireSession(m)` and read the
  tenant via the same `tenantIDFromSession` helper used in tokens
  and connections handlers. (Duplicated again here — extracting to
  a shared package is its own follow-up.)
- `:owner/:name` lookup is tenant-scoped via a new
  `GetRepoByTenantOwnerName` query. Cross-tenant repos are 404, not
  403, mirroring the connections delete behaviour.

### Date range semantics

- Query params `from` and `to` are local dates in `YYYY-MM-DD`
  format. Both are **inclusive**. Missing values default to:
  - `to` = today (in the scheduler's tz; falls back to `time.Local`
    when `cfg.Rollup.Timezone` is unset).
  - `from` = `to - 29` days (a rolling 30-day window).
- Internally, queries use a half-open `[from, to+1)` window because
  the existing list queries (`ListDailyRepoStatsByRepoBetween`,
  etc.) take a `to_date` exclusive boundary. The handler adds one
  day before calling the query.
- Invalid date format → 400 with `"from"` / `"to"` named in the
  error. `from > to` after parsing → 400.
- The range cap is **365 days**. Larger ranges → 400; this keeps a
  single response from accidentally fetching years of per-reviewer
  daily rows.

### Response shapes

`GET /api/v1/repos` → `200 {"repos": [RepoDTO, ...]}` ordered by
`(owner, name)` (existing `ListReposByTenant`). RepoDTO is:

```jsonc
{
  "id": 1,
  "owner": "octocat",
  "name": "hello",
  "default_branch": "main",
  "archived": false
}
```

`tenant_id` / `connection_id` / `gh_id` / `added_at` are not
serialised; the dashboard doesn't need them and they leak
identifiers a tenant doesn't need to see.

`GET /api/v1/repos/:owner/:name/metrics?from=&to=` → 200 with:

```jsonc
{
  "repo": { ... RepoDTO ... },
  "from": "2026-04-19",
  "to":   "2026-05-18",
  "daily_stats": [
    { "date":"2026-04-19",
      "prs_opened":2, "prs_merged":1, "prs_closed":0, "deploys":3,
      "lead_time_seconds_p50":3600, "lead_time_seconds_p90":7200 },
    ...
  ],
  "daily_review_latency": [
    { "date":"2026-04-19",
      "time_to_first_review_seconds_p50":1800,
      "time_to_first_review_seconds_p90":3600,
      "count":5 },
    ...
  ],
  "daily_review_load": [
    { "date":"2026-04-19",
      "reviewer_gh_user_id":12,
      "reviews":3,
      "response_minutes_p50":30 },
    ...
  ]
}
```

The `daily_*` arrays may be sparse — only rollup-populated dates
appear. Frontend interpolates zeros for missing days. Nullable
columns are emitted as JSON `null` (the DTO struct uses `*int64`).

### Scope notes / non-goals

- **No aggregation in the API.** The arrays return per-day rows as
  stored; the frontend sums / averages as needed.
- **No `engineers` rollup data.** Engineer stats belong to 0043's
  endpoint. The per-repo dashboard shows lead time, deploys, review
  latency, and review load; per-engineer breakdowns are a different
  drill-down.
- **No "limit reviewers shown" in review_load.** A repo with 50
  active reviewers in 30 days produces ≤ 1500 rows — manageable.
  If we need it, add a `limit_reviewers` query param later.
- **No date validation against repo creation date.** A query that
  asks for dates before the repo was added simply returns empty
  arrays for those days.

## Acceptance criteria

- [ ] `internal/storage/sqlite/queries/repos.sql` adds
      `-- name: GetRepoByTenantOwnerName :one`:

      ```sql
      SELECT * FROM repos
      WHERE tenant_id = @tenant_id AND owner = @owner AND name = @name
      LIMIT 1;
      ```

- [ ] `sqlc generate` clean; the generated `GetRepoByTenantOwnerName`
      compiles.
- [ ] `internal/api/repos/repos.go` exposes
      `Configure(e, l, m, q, cfg)` and mounts both routes behind
      `web.RequireSession(m)`. Date defaults use `cfg.Rollup.Timezone`
      (falling back to `time.Local` when nil) — same `tz()` helper
      shape as `rollup.Scheduler.tz`.
- [ ] DTOs and request/response types are exported so the test file
      can unmarshal cleanly.
- [ ] `internal/api/run.go` wires `repos.Configure(...)` into
      `configureRoutes`.
- [ ] `internal/api/repos/repos_test.go` covers:
      - `TestList_Empty` → 200 with `{"repos":[]}`.
      - `TestList_HappyPath` — seed two repos under the tenant +
        one under another tenant; only the caller's two come back
        in `(owner, name)` order.
      - `TestList_NoCookie_401`.
      - `TestMetrics_HappyPath` — seed a repo + a few daily-rollup
        rows; assert all three arrays come back populated.
      - `TestMetrics_DefaultsTo30Days` — no `from`/`to` query
        params; the response's `from` should be ~29 days before
        today and `to` should be today.
      - `TestMetrics_RespectsRange` — only rows in `[from, to]`
        appear; rows outside are filtered.
      - `TestMetrics_InvalidFromOrTo_400` (`?from=not-a-date`,
        `?to=not-a-date`, and `from=today, to=yesterday`).
      - `TestMetrics_RangeTooLong_400` — `to - from > 365 days` → 400.
      - `TestMetrics_RepoNotFound_404`.
      - `TestMetrics_CrossTenantRepo_404` — seed a repo under
        tenant B with the same owner/name as something tenant A
        might guess; tenant A's GET returns 404 and does not leak
        existence (no fields from tenant B's repo).
      - `TestMetrics_ArchivedRepoStillVisible` — archived repos
        keep their historical metrics; the endpoint still serves
        them.
      - `TestMetrics_NoCookie_401`.
- [ ] `go vet`, `go build`, `go test ./internal/...` pass.
- [ ] `verify.sh` runs the standard four sections.

## Files to touch

- `internal/storage/sqlite/queries/repos.sql` — new query.
- `internal/storage/sqlite/sqlitedb/*` — regenerated by sqlc.
- `internal/api/repos/repos.go` (new).
- `internal/api/repos/repos_test.go` (new).
- `internal/api/run.go` — register Configure.
- `.plans/upnext/0041-api-repos-metrics/verify.sh`.

## Steps

### 1. sqlc query

Add `GetRepoByTenantOwnerName` to `repos.sql`. Regenerate.

Commit: `feat(storage): repo by owner/name lookup (#0041)`.

### 2. Handler package

Create `internal/api/repos/repos.go`:

- `RepoDTO`, `ListReposResponse`, `MetricsResponse`,
  `DailyRepoStatDTO`, `DailyReviewLatencyDTO`, `DailyReviewLoadDTO`.
- `Configure(e, l, m, q, cfg)` mounts both routes.
- `listHandler(q)` → list tenant's repos, map to DTOs.
- `metricsHandler(q, cfg)` → parse dates, look up repo, fetch the
  three daily series, return JSON.
- Local `tenantIDFromSession` helper (lifted from tokens/connections).
- Local `tzFromCfg(cfg)` helper — same shape as `rollup.tz()`.

Commit: `feat(api): repos list + metrics handlers (#0041)`.

### 3. Wire into router

In `internal/api/run.go`, add `repos.Configure(e, l, m, q, cfg)`
alongside `connections.Configure`. No new constructor args needed —
`cfg` already plumbed in 0040.

Commit (combined with handler).

### 4. Tests

Create `internal/api/repos/repos_test.go`. Lift the test harness
shape from `connections_test.go`:
- `newIntegrationStore`, `seedAndLogin`, `doJSON`,
  `tenantIDForLogin`, `defaultCfg`.
- `seedRepo(t, q, tenantID, owner, name)` →
  `q.CreateRepo(...)` returning the persisted row.
- `seedDailyRepoStats(t, q, repoID, date, opts...)` —
  `q.UpsertDailyRepoStats(...)` with sensible defaults.
- `seedDailyReviewLatency(t, q, repoID, date, p50, p90, count)`.
- `seedDailyReviewLoad(t, q, repoID, date, reviewerUID, reviews,
  p50minutes)`.

One func per acceptance-criteria bullet.

Commit: `test(api): repos list + metrics coverage (#0041)`.

### 5. Verify

`./verify.sh`.

## Notes

- The metrics endpoint serves data sourced exclusively from the
  daily rollup tables, not the raw events. That's deliberate: the
  rollup is small + fast and the dashboard never wants to scan raw
  PRs for analytics. If a tenant wants to backfill rollups for a
  new connection, 0037's `Scheduler.Rebuild` handles that.
- Date defaulting reads `cfg.Rollup.Timezone`. If unset, falls back
  to `time.Local`. Tests can pin tz via `defaultCfg`.
- 365-day cap is a sanity guard, not a contract. The daily tables
  are tiny per repo (~365 rows × small column set), but per-reviewer
  rows in `daily_review_load` multiply by the active-reviewer count.
  365 days is enough for "annual review" pulls without dragging in
  multi-year data.
