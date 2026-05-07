# 0001 — repo-scaffolding

## What changed

Created top-level scaffolding files:

- `.mise.toml` — pins Go 1.24, Node 24, pnpm 10.
- `Makefile` — canonical targets (`help`, `dev`, `build`, `test`, `lint`, `fmt`, `ci`, `clean`); bodies are stubs to be filled in by Tasks 0005/0006.
- `LICENSE` — MIT, 2026, "karnstack contributors".
- `.gitignore` — Go binaries, `dist/`, `node_modules/`, sqlite artifacts, `data/`, `.air-tmp/`, editor scratch, local env.
- `.editorconfig` — utf-8/lf, 2-space default, tab for `*.go` and `Makefile`, no trim on `*.md`.
- `.golangci.yml` — conservative default linter set (errcheck, govet, ineffassign, gosimple, unused, gofmt, goimports, staticcheck).

`README.md` was left unchanged (already committed by hand).

## Verify output

```
verify ok
```

## Followups

- Makefile bodies for `dev`/`build` get filled in by Tasks 0005 (embed SPA) and 0006 (Air dev tooling).
- `.golangci.yml` is intentionally minimal until there's Go code to lint (Task 0002 onward).
