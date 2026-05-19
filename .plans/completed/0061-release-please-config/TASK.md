---
id: 0061
slug: release-please-config
title: release-please config
status: done
depends_on: [0058, 0060]
owner: ""
est_minutes: 30
tags: [ci, release]
autonomy: full
skills: []
---

## Goal

Wire `release-please` for tempo's conventional-commit-driven
release flow:

1. `release-please-config.json` — single Go module at repo root.
2. `.release-please-manifest.json` — `{ ".": "0.0.0" }` seed.
3. `.github/workflows/release-please.yml` — runs the v4 action on
   push to main; when a release PR merges, builds + pushes
   `ghcr.io/karnstack/tempo:<tag>` and `:latest`.

## Acceptance criteria

- [ ] `release-please-config.json` with `release-type: go`,
      `include-component-in-tag: false`.
- [ ] `.release-please-manifest.json` seeded to 0.0.0.
- [ ] release-please.yml runs on push-to-main, conditionally
      builds + pushes the Docker image to GHCR on release.
- [ ] verify.sh: all three files parse.

## Files

- `release-please-config.json`.
- `.release-please-manifest.json`.
- `.github/workflows/release-please.yml`.
- `.plans/upnext/0061-release-please-config/verify.sh`.

## Steps

1. Author files.
2. Verify.
3. Commit.
