---
id: 0043
slug: api-engineers-metrics
title: /api/v1/engineers + per-engineer metrics
status: done
depends_on: [0033, 0036]
owner: ""
est_minutes: 45
tags: [api]
autonomy: full
skills: []
---

## Goal

Add the two engineer endpoints from
`docs/superpowers/specs/2026-05-08-tempo-design.md:224-225`:

```
GET /api/v1/engineers
GET /api/v1/engineers/:login/metrics?from=&to=
```

Engineers are `gh_users` rows for the caller's tenant. The list
endpoint powers the engineer-search drop-down; the metrics endpoint
returns the per-engineer time-series the profile page renders
(commits / PRs / reviews authored per day, broken down per repo,
plus the engineer's review-load fan-out).

### Auth / range / scope

- Behind `web.RequireSession(m)`, tenant resolved via the standard
  helper.
- Same date-range parsing rules as 0041/0042. Parser duplication
  continues (this is the fourth handler); deferred extraction.
- Cross-tenant engineers return 404.

### Response shapes

`GET /api/v1/engineers` →

```jsonc
{ "engineers": [
  { "id":12, "gh_id":1234, "login":"octocat",
    "name":"Octo Cat", "avatar_url":"https://..." }
] }
```

Ordered by `login`. `tenant_id`, `last_seen_at` not surfaced.

`GET /api/v1/engineers/:login/metrics` returns:

```jsonc
{
  "engineer": { ... },
  "from": "...", "to": "...",
  "daily_stats": [          // per (date, repo)
    { "date":"2026-04-19", "repo_id":1,
      "commits":3, "prs_opened":1, "prs_merged":2,
      "reviews_given":4, "comments":5,
      "additions":120, "deletions":40 }
  ],
  "daily_review_load": [
    { "date":"2026-04-19", "repo_id":1, "reviews":5,
      "response_minutes_p50":30 }
  ]
}
```

## Acceptance criteria

- [ ] `internal/storage/sqlite/queries/gh_users.sql` adds
      `GetGhUserByTenantLogin :one`.
- [ ] `internal/api/engineers/engineers.go` mounts:
      `GET /api/v1/engineers` and `GET /api/v1/engineers/:login/metrics`.
- [ ] DTOs exported; EngineerDTO never carries tenant_id /
      last_seen_at.
- [ ] `internal/api/run.go` wires `engineers.Configure(...)`.
- [ ] `internal/api/engineers/engineers_test.go` covers list (empty,
      ordered, cross-tenant excluded, 401), metrics (happy, range
      filter, default-30d, invalid dates, not-found, cross-tenant
      404, 401).
- [ ] `verify.sh` clean.

## Files to touch

- `internal/storage/sqlite/queries/gh_users.sql`.
- Regenerated sqlc files.
- `internal/api/engineers/engineers.go` (new).
- `internal/api/engineers/engineers_test.go` (new).
- `internal/api/run.go`.
- `.plans/upnext/0043-api-engineers-metrics/verify.sh`.

## Steps

1. **sqlc query** — `GetGhUserByTenantLogin`. Commit
   `feat(storage): gh_user by tenant+login lookup (#0043)`.
2. **Handler** — `engineers.go` with both routes. Reuse date-parse
   shape from orgs handler. Commit
   `feat(api): engineers list + metrics handler (#0043)`.
3. **Router wiring** — `engineers.Configure(e, l, m, q, cfg)`.
4. **Tests** — mirror orgs test shape. Commit
   `test(api): engineers coverage (#0043)`.
5. **Verify** — `./verify.sh`.

## Notes

- The metrics endpoint pulls authored stats from
  `daily_engineer_stats` and load fan-out from
  `daily_review_load`, both keyed by `gh_user_id`.
- Per-day rows are emitted in `(date, repo_id)` order so the
  frontend can render directly. Lead-time / latency percentile data
  stays at the repo endpoint.
- Existing queries
  (`ListDailyEngineerStatsByUserBetween`,
  `ListDailyReviewLoadByReviewerBetween`) already match the shape;
  the only new SQL is the user lookup.
