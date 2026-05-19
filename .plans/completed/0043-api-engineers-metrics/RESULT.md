# 0043 — /api/v1/engineers + per-engineer metrics

## Files changed

- `internal/storage/sqlite/queries/gh_users.sql` — added
  `GetGhUserByTenantLogin :one`.
- `internal/storage/sqlite/sqlitedb/*` — regenerated.
- `internal/api/engineers/engineers.go` — new package with
  `GET /api/v1/engineers` + `GET /api/v1/engineers/:login/metrics`.
- `internal/api/engineers/engineers_test.go` — 12 integration tests.
- `internal/api/run.go` — `engineers.Configure(...)` wired.

## Verify output

```
== sqlc diff ==
== go vet ==
== go build ==
== go test (api) ==
ok  	github.com/karnstack/tempo/internal/api/engineers (cached)
... all api packages OK
```

## Notes / followups

- **Date parser + tenantIDFromSession duplicated 4x now.** Tokens
  uses one shape, connections/repos/orgs/engineers all repeat the
  date-parse + tenant-helper pattern. Extract to
  `internal/api/web/` as a small follow-up; lifting now would
  bloat this task with cross-package churn.
- **Authored stats and review fan-out, separately.** The metrics
  response surfaces per-`(date, repo)` rows from both
  `daily_engineer_stats` and `daily_review_load`. Aggregating into
  one combined row would lose the per-repo dimension; the
  frontend reduces as needed.
- **No "ghost" filter.** `gh_users` never gets a row with
  `gh_id = 0` (the commits-ingest Ghost sentinel uses the literal
  UID 0, not a stored row), so a request for `/engineers/Ghost`
  naturally returns 404.
