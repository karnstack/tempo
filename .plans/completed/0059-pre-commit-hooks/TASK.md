---
id: 0059
slug: pre-commit-hooks
title: Pre-commit hooks (gofmt, golangci, pnpm typecheck/lint)
status: done
depends_on: [0058]
owner: ""
est_minutes: 30
tags: [ci]
autonomy: full
skills: []
---

## Goal

`.pre-commit-config.yaml` running the same checks CI runs, locally
on staged files. Plus `make pre-commit-install`.

Hooks: gofmt/goimports, go vet, golangci-lint (non-blocking),
prettier + eslint over `web/src/**`, pnpm typecheck (project-wide),
trailing-whitespace / end-of-file / check-yaml from pre-commit-hooks.

## Acceptance criteria

- [ ] `.pre-commit-config.yaml` with pinned hook revs.
- [ ] `make pre-commit-install` target.
- [ ] verify.sh: file exists + YAML parses.

## Files

- `.pre-commit-config.yaml`.
- `Makefile`.
- `.plans/upnext/0059-pre-commit-hooks/verify.sh`.

## Steps

1. Config.
2. Make target.
3. Verify.
