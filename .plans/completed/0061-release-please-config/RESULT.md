# 0061 — release-please config

## Files changed

- `release-please-config.json` — single Go module config, clean
  `vX.Y.Z` tags.
- `.release-please-manifest.json` — `0.0.0` seed (first PR cuts
  `v0.1.0`).
- `.github/workflows/release-please.yml` — release-please-action@v4
  on push-to-main + conditional GHCR multi-arch publish on
  release.

## Verify output

```
  release-please-config.json ok
  .release-please-manifest.json ok
  .github/workflows/release-please.yml ok
  release-please-config.json parses
  .release-please-manifest.json parses
  .github/workflows/release-please.yml parses
VERIFY OK
```

## Notes / followups

- **GHCR over Docker Hub.** No external credentials; uses
  `GITHUB_TOKEN` automatically scoped to `packages: write`.
- **Multi-arch publish on release only.** CI's docker job (0058)
  is single-arch + no-push, since per-PR multi-arch builds slow
  the loop. Multi-arch happens at the release boundary.
- **First release will scan full history.** No `bootstrap-sha`
  override — conventional-commit prefixes have been disciplined
  throughout 0001-0060 so the v0.1.0 changelog will read cleanly.
- **`bump-minor-pre-major: true`** so `feat:` commits before 1.0.0
  bump the minor (0.1.0 → 0.2.0). After 1.0.0 they'd bump the
  patch by default; that's the standard pre-1.0 convention.
