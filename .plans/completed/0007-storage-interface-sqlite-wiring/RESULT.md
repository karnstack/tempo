# 0007 — Storage interface + SQLite driver wiring (sqlc, goose) — RESULT

## What changed

- **`.mise.toml`** — pin `sqlc = "1.31.1"`.
- **`go.mod` / `go.sum`** — add `modernc.org/sqlite v1.50.0`, `github.com/pressly/goose/v3 v3.27.1` (pure Go, no CGo).
- **`internal/config/config.go`** — `Database{Driver, DSN, Raw}` field on `Config`. `parseDB()` recognises `sqlite://`, `postgres://`, `postgresql://`. Unknown schemes panic at boot.
- **`internal/config/config_test.go`** — six-case table test.
- **`internal/storage/storage.go`** — `Storage` interface (`DB() *sql.DB`, `Ping(ctx)`, `Close()`).
- **`internal/storage/sqlite/sqlite.go`** — fx provider `New(lc, l, cfg)` opens via `modernc.org/sqlite`, applies `journal_mode=WAL`, `synchronous=NORMAL`, `foreign_keys=ON`, `busy_timeout=5000`, `temp_store=MEMORY` via `_pragma=` query params, verifies `foreign_keys` took effect, pings, registers OnStop close. Pool capped at 8 open / 4 idle.
- **`internal/storage/sqlite/sqlite_test.go`** — three tests: in-memory open + ping + foreign_keys; tempfile WAL + busy_timeout=5000; rejects non-sqlite driver.
- **`internal/storage/postgres/postgres.go`** — stub package with `Open() (storage.Storage, error)` returning "not implemented in v1".
- **`cmd/migrate/main.go`** — pure-Go goose runner (`up`/`down`/`status`/`version`) that links `modernc.org/sqlite` and `pressly/goose/v3` directly. No CGo, no separate goose CLI.
- **`sqlc.yaml`** — engine `sqlite`, schema `migrations`, queries `internal/storage/sqlite/queries`, gen package `sqlitedb` → `internal/storage/sqlite/sqlitedb`.
- **`internal/storage/sqlite/queries/.gitkeep`**, **`migrations/.gitkeep`**, **`migrations/README.md`** — seed dirs.
- **`Makefile`** — `migrate-up`, `migrate-down`, `migrate-status`, `sqlc-generate` targets + `.PHONY` updated.
- **`cmd/tempo/main.go`** — `fx.Provide(logger.New, config.Load, sqlite.New)` + `fx.Invoke(touchStorage)` to force SQLite open + ping at boot.

## Verify output

```
$ ./.plans/upnext/0007-storage-interface-sqlite-wiring/verify.sh
... (logs elided) ...
storage warmup ok
starting tempo api  addr=:18278
http server started on [::]:18278
received signal  signal=TERMINATED
closing sqlite db
verify ok
```

`go test ./...` — `internal/config` ok, `internal/storage/sqlite` ok, every other package has no tests yet.

End-to-end boot: `TEMPO_DB=sqlite://<tmp>/tempo.db ./tempo` opens the DB at the configured path, pings, serves `/api/v1/system/health` with `"status":"ok"`, shuts down cleanly via OS signal — fx triggers OnStop in the right order (sqlite close → http shutdown → logger sync).

## Followups (intentionally deferred)

- **0008–0011** — actual schema migrations (`migrations/0001_*.sql` etc.). `make migrate-up` is wired and ready.
- **0012** — sqlc-generated repo methods. Drop `.sql` files into `internal/storage/sqlite/queries/`, run `make sqlc-generate`, hang typed methods off `storage.Storage`.
- **0013** — `cfgx` swaps the panic-on-bad-`TEMPO_DB` for a typed error path.
- **0016+** — auth/ingest/rollup replace the `touchStorage` no-op with real consumers.

## Notes

- Goose 3.27.1 errors `failed to collect migrations: no migration files found` when the dir is empty, so the runtime `make migrate-status` smoke check moves to 0008's `verify.sh`. Compile-check stays in 0007.
- `sqlc generate` against an empty queries dir exits 1 by design — that gate moves to 0012's `verify.sh`.
- Driver name is registered as `sqlite` (modernc default), not `sqlite3`. Goose's `SetDialect("sqlite3")` is the SQL dialect selector, separate from the driver name.
