---
id: 0042
slug: api-orgs-metrics
title: /api/v1/orgs/:org/metrics
status: done
depends_on: [0041]
owner: ""
est_minutes: 45
tags: [api]
autonomy: full
skills: []
---

## Goal

Add `GET /api/v1/orgs/:org/metrics?from=&to=` from
`docs/superpowers/specs/2026-05-08-tempo-design.md:223`.

The org dashboard's job is "drill down by repo" — so the response
gives the list of repos in the org for the caller's tenant, plus
**summed** daily counts across those repos. Percentile columns
don't aggregate statistically and are intentionally omitted at the
org level — the frontend uses the repo endpoint for per-repo
percentile data.

### Auth / range semantics

- Behind `web.RequireSession(m)`, same `tenantIDFromSession` helper.
- Date range parsing reuses the rules from 0041: YYYY-MM-DD,
  inclusive, defaults to 30-day rolling window, capped at 365 days,
  same tz handling. Extract the parser into
  `internal/api/repos/repos.go`'s package as an unexported helper —
  no, better, inline the parsing in this handler too. Three callers
  is the threshold for extraction; 0043 will likely be #3 and can
  trigger the move.
- `:org` is the GitHub login (the `owner` column on repos). 404 if
  the tenant has zero repos under that owner.

### Response shape

```jsonc
{
  "org": "acme",
  "from": "2026-04-19",
  "to":   "2026-05-18",
  "repos": [RepoDTO, ...],     // every non-archived + archived repo under the owner
  "daily_stats": [             // SUM across org repos
    { "date":"2026-04-19",
      "prs_opened":12, "prs_merged":7, "prs_closed":2, "deploys":4 }
  ],
  "daily_review_latency": [    // SUM of count only (p50/p90 omitted)
    { "date":"2026-04-19", "count":18 }
  ],
  "daily_review_load": [        // SUM of reviews per (date, reviewer)
    { "date":"2026-04-19", "reviewer_gh_user_id":12, "reviews":15 }
  ]
}
```

Percentile fields are intentionally absent from `daily_stats`,
`daily_review_latency`, and `daily_review_load` at the org level —
the underlying values are per-repo p50/p90 from the rollup tables,
and combining them across repos without raw samples would mislead.

### Aggregation strategy

Pre-collapse in SQL via three new queries that JOIN with `repos`
filtered by `(tenant_id, owner)`. Names mirror existing patterns:

- `SumDailyRepoStatsByTenantOwnerBetween :many` — returns
  `(date, prs_opened, prs_merged, prs_closed, deploys)` grouped by
  date.
- `SumDailyReviewLatencyByTenantOwnerBetween :many` — returns
  `(date, count)` grouped by date.
- `SumDailyReviewLoadByTenantOwnerBetween :many` — returns
  `(date, reviewer_gh_user_id, reviews)` grouped by
  `(date, reviewer)`.

All three GROUP BY excluded percentile/p50 columns. SQL `SUM()` on
`INTEGER NOT NULL` columns returns int64 via the driver, no
interface{} mess like 0036's MIN().

### Scope notes

- **No percentile aggregation.** Documented above. If someone
  later wants a "fleet-wide cycle time", do it from raw events,
  not by averaging p50s.
- **No 404 on empty repos list with data.** A tenant with the org
  enrolled but zero rollup rows for the date range returns 200
  with empty arrays — distinct from a 404 "no such org".
- **Archived repos included.** Same logic as the repo endpoint —
  org dashboards summarise history.

## Acceptance criteria

- [ ] `internal/storage/sqlite/queries/daily_repo_stats.sql` adds
      `SumDailyRepoStatsByTenantOwnerBetween :many`.
- [ ] `internal/storage/sqlite/queries/daily_review_latency.sql` adds
      `SumDailyReviewLatencyByTenantOwnerBetween :many`.
- [ ] `internal/storage/sqlite/queries/daily_review_load.sql` adds
      `SumDailyReviewLoadByTenantOwnerBetween :many`.
- [ ] sqlc generates cleanly; the resulting types use int64 for the
      summed columns.
- [ ] `internal/api/orgs/orgs.go` exposes `Configure(e, l, m, q,
      cfg)` and mounts `GET /api/v1/orgs/:org/metrics` behind
      `web.RequireSession`.
- [ ] DTOs are exported. `OrgMetricsResponse` shape matches the
      example above.
- [ ] `internal/api/run.go` wires `orgs.Configure(...)`.
- [ ] `internal/api/orgs/orgs_test.go` covers:
      - `TestMetrics_HappyPath` — two repos under the org, each
        with daily rollups; response sums correctly.
      - `TestMetrics_NoSuchOrg_404`.
      - `TestMetrics_DefaultRange30Days`.
      - `TestMetrics_InvalidDates_400` (bad from, bad to,
        from>to).
      - `TestMetrics_RangeTooLong_400`.
      - `TestMetrics_RespectsRange`.
      - `TestMetrics_CrossTenantOrgIsolated` — another tenant has
        repos under the same owner; their data must not bleed.
      - `TestMetrics_ReposListIncludesArchived`.
      - `TestMetrics_NoCookie_401`.
- [ ] `go vet`, `go build`, `go test` pass.
- [ ] `verify.sh` runs the standard four sections.

## Files to touch

- `internal/storage/sqlite/queries/daily_repo_stats.sql` — sum query.
- `internal/storage/sqlite/queries/daily_review_latency.sql` — sum.
- `internal/storage/sqlite/queries/daily_review_load.sql` — sum.
- `internal/storage/sqlite/sqlitedb/*` — regenerated.
- `internal/api/orgs/orgs.go` (new).
- `internal/api/orgs/orgs_test.go` (new).
- `internal/api/run.go` — register Configure.
- `.plans/upnext/0042-api-orgs-metrics/verify.sh`.

## Steps

### 1. sqlc queries

Three SUM-by-owner queries. Keep comments ASCII (sqlc-sqlite hates
em-dashes, lesson from 0034/0035/0036).

```sql
-- name: SumDailyRepoStatsByTenantOwnerBetween :many
SELECT s.date AS date,
       SUM(s.prs_opened) AS prs_opened,
       SUM(s.prs_merged) AS prs_merged,
       SUM(s.prs_closed) AS prs_closed,
       SUM(s.deploys) AS deploys
FROM daily_repo_stats s
JOIN repos r ON r.id = s.repo_id
WHERE r.tenant_id = @tenant_id AND r.owner = @owner
  AND s.date >= @from_date AND s.date < @to_date
GROUP BY s.date
ORDER BY s.date;
```

Same pattern for the other two; use `SUM(count) AS count` and
`SUM(reviews) AS reviews` respectively. Test the generated row
types are int64 (not interface{}) — adjust with explicit `CAST(...
AS INTEGER)` if needed.

Commit: `feat(storage): org-level summed rollup queries (#0042)`.

### 2. Handler

Create `internal/api/orgs/orgs.go`. Reuse the date-parsing logic
shape from 0041 (`parseDateRange`, `tzFromCfg`, `tenantIDFromSession`).
The 404-on-empty-repos check: call `ListReposByTenant` filtered to
`owner == :org`, return 404 if zero matches.

Commit: `feat(api): org metrics handler (#0042)`.

### 3. Wire + tests

Add to `internal/api/run.go`. Tests mirror the 0041 test file
structure with org-specific seeding (two repos same owner, daily
rollups).

Commit: `test(api): org metrics coverage (#0042)`.

### 4. Verify

Standard `./verify.sh`.

## Notes

- The org endpoint returns RepoDTOs that match the shape from 0041
  exactly (id, owner, name, default_branch, archived). To stay DRY
  without coupling, define a tiny local DTO type — Go's structural
  JSON encoding will still produce identical output, and we avoid
  a cross-package type dependency that'd be brittle if either DTO
  evolves.
- If sqlc-sqlite complains about typed SUM columns (similar trap to
  0036's MIN(timestamp)), the fix is `CAST(SUM(...) AS INTEGER)` —
  forces an int64 scan target.
