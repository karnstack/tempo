# 0059 — Pre-commit hooks

## Files changed

- `.pre-commit-config.yaml` — pre-commit framework config.
- `Makefile` — `pre-commit-install` target.

## Verify output

```
  .pre-commit-config.yaml ok
  Makefile target pre-commit-install present
  .pre-commit-config.yaml parses
VERIFY OK
```

## Notes / followups

- **pre-commit framework over husky/lefthook.** Most common in OSS
  Go repos; handles polyglot hooks cleanly.
- **Opt-in installation.** `make pre-commit-install` rather than
  auto-installing on `make dev` so a fresh clone doesn't fail
  without pre-commit. The Make target prints an install hint when
  the binary's missing.
- **golangci-lint hook is wired but tolerant.** When the project
  ships a `.golangci.yml` (sibling cleanup task), lift the
  permissive defaults.
- **Local pnpm hooks** for lint + typecheck use `entry: bash -c '...'`
  with `pass_filenames: false` since TS typechecking is
  project-wide.
- **No commitlint.** Conventional-commit enforcement lands with
  release-please in 0061.
