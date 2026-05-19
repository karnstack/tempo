# 0058 — GitHub Actions CI

## Files changed

- `.github/workflows/ci.yml` — three-job CI workflow (go / web /
  docker), concurrency-cancel on PRs, tool versions pinned to
  `.mise.toml`.

## Verify output

```
  .github/workflows/ci.yml ok
  .github/workflows/ci.yml parses
  WARN: actionlint not available; skipping
VERIFY OK
```

## Notes / followups

- **golangci-lint is non-blocking** (`continue-on-error: true`) in
  the initial cut because the repo has no `.golangci.yml`. When
  0059 lands pre-commit hooks that ship a config, lift the
  continue-on-error so CI actually fails on lint issues.
- **Concurrency on PRs only.** `cancel-in-progress` is gated on
  `github.event_name == 'pull_request'` so push-to-main runs
  never get cancelled mid-flight by a follow-up push.
- **actionlint is optional in verify.sh.** Install via
  `go install github.com/rhysd/actionlint/cmd/actionlint@latest`
  for stricter local validation.
- **No `dependabot.yml` here.** Worth adding as a small follow-up;
  out of scope for this task per the TASK body.
- **Docker job's BuildKit cache (`type=gha`)** persists between
  workflow runs on the same branch. First build is slow; later
  builds reuse layers and are fast.
