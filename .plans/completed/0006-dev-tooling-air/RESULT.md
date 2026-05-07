# 0006 — Dev tooling — air for Go hot reload + concurrent dev script

## Summary

`make dev` now boots Go (via `air`) and Vite concurrently. Vite proxies `/api/*` to the Go server on `:8080`, so contributors hit `http://localhost:5173` for HMR while API calls land on the Go binary. `make fmt`, `make lint`, `make test` cover both Go and frontend with graceful skips when tools/scripts are missing.

## Files changed

- `.air.toml` (new) — air config: builds `./.air-tmp/tempo` from `./cmd/tempo`, excludes `web/`, `internal/webui/dist/`, `node_modules/`, `.plans/`, `docs/`, `data/`, and the temp dir; `clean_on_exit = true`.
- `web/vite.config.ts` — added `server.proxy` mapping `/api` → `http://localhost:8080`.
- `Makefile` — `dev`, `fmt`, `lint`, `test` filled in:
  - `dev` checks for `air` and runs `air` + `pnpm -C web dev` under a Ctrl-C trap.
  - `fmt` runs `go fmt`, `goimports` (if installed), `prettier --write` (if available).
  - `lint` runs `golangci-lint run`, `pnpm run lint`, `pnpm run typecheck`.
  - `test` runs `go test ./...`, then frontend `test` script if defined (skipped gracefully today).

## Verify output

```
$ ./.plans/upnext/0006-dev-tooling-air/verify.sh
verify ok
```

`make -n dev` parses cleanly:

```
command -v air >/dev/null || (echo "install air: go install github.com/air-verse/air@latest" && exit 1)
echo "  Go    → http://localhost:8080"
echo "  Vite  → http://localhost:5173 (proxies /api → :8080)"
trap 'kill 0' INT TERM; \
		air & \
		pnpm -C web dev & \
		wait
```

## Followups

- `air` is not bundled. Contributors install it manually via `go install github.com/air-verse/air@latest`. The `dev` target prints the install command on missing-tool error. Could be added to a `bootstrap` make target later.
- Frontend test runner has no `test` script in `web/package.json` yet — `make test` skips it with a notice. Wire up vitest when the first frontend tests land.
- `pnpm -C web run lint` and `typecheck` are tolerated to fail (`|| true`) so contributors don't get blocked on transitively introduced issues during bootstrap. Tighten this once the SPA stabilizes.
