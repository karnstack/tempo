---
id: 0044
slug: api-sync-status-health
title: /api/v1/sync/status + system/health
status: done
depends_on: [0031]
owner: ""
est_minutes: 30
tags: [api]
autonomy: full
skills: []
---

## Goal

Add `GET /api/v1/sync/status` (auth required) returning per-connection
ingest health for the caller's tenant. `/system/health` is already
mounted publicly by `internal/api/health/`.

Response surfaces, for each connection: identifier fields plus the
`latest_run`, `last_success`, `last_failure` snapshots from
`ingest.StatusFor`. Missing run kinds emit JSON null.

## Acceptance criteria

- [ ] `internal/api/sync/sync.go` mounts `GET /api/v1/sync/status`
      behind RequireSession with exported DTOs.
- [ ] `internal/api/run.go` wires `sync.Configure(...)`.
- [ ] Tests cover empty / happy / no-sync-runs / cross-tenant /
      no-cookie cases.

## Files

- `internal/api/sync/sync.go` (new).
- `internal/api/sync/sync_test.go` (new).
- `internal/api/run.go`.

## Steps

1. Handler — commit `feat(api): sync status handler (#0044)`.
2. Tests — commit `test(api): sync status coverage (#0044)`.
3. Verify.
