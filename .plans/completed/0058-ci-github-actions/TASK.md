---
id: 0058
slug: ci-github-actions
title: GitHub Actions CI
status: done
depends_on: [0046, 0012]
owner: ""
est_minutes: 45
tags: [ci]
autonomy: full
skills: []
---

## Goal

`.github/workflows/ci.yml` running standard checks on push-to-main
and PRs:

1. Go: setup-go (cache), sqlc diff, `make openapi-validate`,
   go vet, golangci-lint (non-blocking initially), tests, build.
2. Web: setup-node + pnpm, `pnpm install --frozen-lockfile`,
   lint, typecheck, build, `make openapi-check-frontend`.
3. Docker: build the image (no push) — verifies the Dockerfile
   stays buildable.

Concurrency-cancel-in-progress so superseded PR runs don't pile up.

Versions pinned to `.mise.toml`:

- go 1.26.3
- node 24.15.0
- pnpm 10.33.4
- sqlc 1.31.1

## Acceptance criteria

- [ ] `.github/workflows/ci.yml` exists with three jobs.
- [ ] Push to main + PR triggers; concurrency cancels superseded.
- [ ] Each job pins its tooling to the .mise.toml patch level.
- [ ] verify.sh: file exists + YAML parses via the gopkg.in/yaml.v3
      one-shot (matches 0057's pattern).

## Files

- `.github/workflows/ci.yml` (new).
- `.plans/upnext/0058-ci-github-actions/verify.sh`.

## Steps

1. Author workflow.
2. Validate YAML.
3. Commit.

## Notes

- `pnpm/action-setup` BEFORE `setup-node` so the latter picks up
  pnpm for its cache config.
- `golangci-lint run` non-blocking (`continue-on-error: true`) for
  this initial cut — there's no `.golangci.yml` yet. Lift the
  override after 0059's pre-commit hooks land a config.
- No registry push / release — 0061's job.
- No multi-OS matrix; Linux only for v1.
