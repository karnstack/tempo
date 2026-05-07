---
id: 0007
slug: storage-interface-sqlite-wiring
title: Storage interface + SQLite driver wiring (sqlc, goose)
status: done
depends_on: [0002]
owner: ""
est_minutes: 60
tags: [storage, sqlite]
autonomy: full
skills: []
---

## Goal

Wire SQLite into the running fx process and lay down the tooling rails for migrations + typed queries that the next several tasks will use. After this task:

- The binary opens a SQLite database at `TEMPO_DB` on startup, applies sane PRAGMAs (WAL, FK on, busy timeout), pings it, and closes it cleanly on shutdown.
- A minimal `storage.Storage` interface (the seam) exists; `internal/storage/sqlite` provides the SQLite implementation and an fx provider.
- `sqlc.yaml` is in place so 0012 can drop in `.sql` query files and run `make sqlc-generate`.
- `cmd/migrate/main.go` runs goose-as-a-library against the same `TEMPO_DB`, with **no CGo** anywhere — pure-Go SQLite via `modernc.org/sqlite`. 0008–0011 will simply add `.sql` files under `migrations/` and `make migrate-up` will work.

No actual schema, no actual queries. Those land in 0008+. This task proves the rails work end to end.

## Stack decisions (locked)

- **SQLite driver**: `modernc.org/sqlite v1.50.0` (pure Go). Keeps distroless static builds (task 0060) trivial — no `gcc` required. Driver name registered as `sqlite`.
- **Migrations**: `github.com/pressly/goose/v3 v3.27.1` used as a **library** from `cmd/migrate`. We do _not_ install the goose CLI — that path requires CGo for SQLite. Instead, `make migrate-up` invokes `go run ./cmd/migrate up`.
- **Typed queries**: `sqlc 1.31.1` installed via `mise`. Tools added to `[tools]` in `.mise.toml` (per memory: pin patch level).
- **Seam**: `internal/storage/storage.go` exposes a minimal `Storage` interface — `DB() *sql.DB`, `Ping(ctx) error`, `Close() error`. Repos plug in via 0012; for now this is enough to wire fx and prove the seam.
- **DSN format**: `TEMPO_DB=sqlite://<path>` (e.g. `sqlite://./data/tempo.db`, or `sqlite://:memory:` for tests). Postgres support stubbed (returns `not implemented`) so 0013+ can extend without churn.

## Acceptance criteria

- [ ] `internal/config/config.go` accepts `TEMPO_DB` (default `sqlite://./data/tempo.db`), exposes a parsed `Database` value with `Driver` (`"sqlite"`) and `DSN` (filesystem path or `:memory:`). Unknown schemes error out at parse time.
- [ ] `internal/storage/storage.go` declares `Storage` interface with `DB() *sql.DB`, `Ping(ctx context.Context) error`, `Close() error`.
- [ ] `internal/storage/sqlite/sqlite.go` implements `Storage` and exposes `New(lc fx.Lifecycle, l *zap.Logger, cfg *config.Config) (storage.Storage, error)` for fx.
- [ ] On open, the SQLite driver applies these PRAGMAs in this order: `journal_mode=WAL`, `synchronous=NORMAL`, `foreign_keys=ON`, `busy_timeout=5000`, `temp_store=MEMORY`. PRAGMAs are verified via `SELECT PRAGMA` on a quick smoke check at open time.
- [ ] `internal/storage/postgres/postgres.go` exists as a stub package with a doc comment + `Open` returning `errors.New("postgres backend not implemented in v1")`.
- [ ] `cmd/tempo/main.go` registers `fx.Provide(sqlite.New)` and exposes the `Storage` to the graph (no business code uses it yet — `fx.Invoke` a no-op `Touch(storage.Storage)` so fx instantiates it). Startup logs include `sqlite db ready path=<dsn>`.
- [ ] `cmd/migrate/main.go` exists. It opens the same DB the server would (reads `TEMPO_DB`), uses goose's library API, and supports `up`, `down`, `status`, `version` subcommands. `make migrate-up` and `make migrate-down` invoke it.
- [ ] `sqlc.yaml` exists at repo root: engine `sqlite`, schema dir `migrations`, queries dir `internal/storage/sqlite/queries`, generated package `sqlitedb`, output `internal/storage/sqlite/sqlitedb`. `sqlc generate` runs without error against an empty queries dir (it should be a no-op, not a failure).
- [ ] `migrations/` exists with `.gitkeep` and a one-line README pointing readers at `make migrate-up`.
- [ ] `internal/storage/sqlite/queries/.gitkeep` exists so the dir is tracked.
- [ ] `data/` is created on first run if it doesn't exist (parent dir of the SQLite file). `.gitignore` already covers `data/`.
- [ ] `.mise.toml` adds `sqlc = "1.31.1"`.
- [ ] `Makefile` gains `migrate-up`, `migrate-down`, `migrate-status`, `sqlc-generate` targets.
- [ ] `go test ./internal/storage/...` passes — covers the open path, PRAGMAs, ping, close, against `:memory:`.
- [ ] The `tempo` binary still builds (`make build`) and the existing 0002/0005/0006 verify behaviour is unchanged.
- [ ] `verify.sh` passes.

## Files to touch

Create:

- `internal/storage/sqlite/sqlite.go`
- `internal/storage/sqlite/sqlite_test.go`
- `internal/storage/postgres/postgres.go`
- `internal/storage/sqlite/queries/.gitkeep`
- `cmd/migrate/main.go`
- `sqlc.yaml`
- `migrations/.gitkeep`
- `migrations/README.md`

Modify:

- `internal/storage/storage.go` (currently a one-line stub) — replace with `Storage` interface.
- `internal/config/config.go` — add `Database` parsing (`Driver`, `DSN`).
- `cmd/tempo/main.go` — `fx.Provide(sqlite.New)` + a no-op `Touch` invocation so the storage is instantiated and pinged at startup.
- `Makefile` — add `migrate-up`, `migrate-down`, `migrate-status`, `sqlc-generate`.
- `.mise.toml` — pin `sqlc = "1.31.1"`.
- `go.mod` / `go.sum` — `modernc.org/sqlite v1.50.0`, `github.com/pressly/goose/v3 v3.27.1`.

## Steps

> Each step ends with a small commit. Per memory, pin to exact patch versions when adding deps.

- [ ] **Step 1 — Pin sqlc in `.mise.toml`**

  ```toml
  [tools]
  go = "1.26.3"
  node = "24.15.0"
  pnpm = "10.33.4"
  sqlc = "1.31.1"
  ```

  Then `mise install` (best-effort — this is for contributor ergonomics; if the runtime doesn't have mise, `sqlc` can also be installed via `go install github.com/sqlc-dev/sqlc/cmd/sqlc@v1.31.1`, but we prefer the mise binary for parity).

  Commit: `chore(tooling): pin sqlc 1.31.1 in .mise.toml`.

- [ ] **Step 2 — Add Go deps**

  ```bash
  go get modernc.org/sqlite@v1.50.0
  go get github.com/pressly/goose/v3@v3.27.1
  go mod tidy
  ```

  Commit: `chore(deps): add modernc.org/sqlite + goose v3`.

- [ ] **Step 3 — Extend `internal/config/config.go`**

  Add a `Database` field with parsed `Driver`/`DSN`. Default `TEMPO_DB=sqlite://./data/tempo.db`. Reject unknown schemes loudly so 0013's cfgx port has good failure modes to inherit.

  ```go
  type Config struct {
      Listen   string
      Env      string
      Database Database
  }

  type Database struct {
      // Driver is the database/sql driver name. "sqlite" today; "postgres" in the future.
      Driver string
      // DSN is the driver-specific connection string. For sqlite this is a filesystem
      // path or ":memory:". Postgres will use the libpq URL form.
      DSN string
      // Raw preserves the original TEMPO_DB value for logs.
      Raw string
  }

  func Load() *Config {
      raw := getenv("TEMPO_DB", "sqlite://./data/tempo.db")
      db, err := parseDB(raw)
      if err != nil {
          // Match the existing minimal-config pattern: panic at boot rather
          // than smuggle a half-built Config to callers. cfgx in 0013 turns
          // this into a typed error.
          panic(err)
      }
      return &Config{
          Listen:   getenv("TEMPO_LISTEN", ":8080"),
          Env:      getenv("TEMPO_ENV", "development"),
          Database: db,
      }
  }

  func parseDB(raw string) (Database, error) {
      switch {
      case strings.HasPrefix(raw, "sqlite://"):
          return Database{Driver: "sqlite", DSN: strings.TrimPrefix(raw, "sqlite://"), Raw: raw}, nil
      case strings.HasPrefix(raw, "postgres://"), strings.HasPrefix(raw, "postgresql://"):
          return Database{Driver: "postgres", DSN: raw, Raw: raw}, nil
      default:
          return Database{}, fmt.Errorf("config: unsupported TEMPO_DB scheme %q", raw)
      }
  }
  ```

  Add a small unit test in `internal/config/config_test.go` covering the three branches.

  Commit: `feat(config): parse TEMPO_DB into driver+DSN`.

- [ ] **Step 4 — `internal/storage/storage.go`** (replace the stub)

  ```go
  // Package storage hosts the persistence-layer seam. SQLite is the v1 backend;
  // Postgres lives in internal/storage/postgres as a future stub.
  package storage

  import (
      "context"
      "database/sql"
  )

  // Storage is the seam between business code and a concrete database. Repository
  // methods (Users, Connections, etc.) will hang off this interface as 0012 lands
  // sqlc-generated query packages.
  type Storage interface {
      // DB returns the underlying *sql.DB. Used by migrations and ad-hoc queries.
      DB() *sql.DB
      // Ping verifies the connection is alive.
      Ping(ctx context.Context) error
      // Close releases the connection pool.
      Close() error
  }
  ```

  Commit: `feat(storage): declare Storage interface seam`.

- [ ] **Step 5 — `internal/storage/sqlite/sqlite.go`**

  ```go
  // Package sqlite is the SQLite implementation of storage.Storage. It uses the
  // pure-Go modernc.org/sqlite driver so the binary stays CGo-free.
  package sqlite

  import (
      "context"
      "database/sql"
      "fmt"
      "os"
      "path/filepath"
      "strings"
      "time"

      "github.com/karnstack/tempo/internal/config"
      "github.com/karnstack/tempo/internal/storage"
      "go.uber.org/fx"
      "go.uber.org/zap"
      _ "modernc.org/sqlite" // register "sqlite" driver
  )

  // pragmas applied at open time, in order. WAL gives concurrent readers; NORMAL
  // sync is the recommended pairing with WAL; busy_timeout avoids "database is
  // locked" under contention; temp_store=MEMORY moves temp tables off disk.
  var pragmas = []string{
      "journal_mode=WAL",
      "synchronous=NORMAL",
      "foreign_keys=ON",
      "busy_timeout=5000",
      "temp_store=MEMORY",
  }

  // Store implements storage.Storage.
  type Store struct {
      db *sql.DB
  }

  func (s *Store) DB() *sql.DB                        { return s.db }
  func (s *Store) Ping(ctx context.Context) error     { return s.db.PingContext(ctx) }
  func (s *Store) Close() error                       { return s.db.Close() }

  // New is the fx provider. It opens the database, applies PRAGMAs, pings it,
  // and registers an OnStop hook that closes the pool.
  func New(lc fx.Lifecycle, l *zap.Logger, cfg *config.Config) (storage.Storage, error) {
      if cfg.Database.Driver != "sqlite" {
          return nil, fmt.Errorf("sqlite.New: expected driver=sqlite, got %q", cfg.Database.Driver)
      }
      if err := ensureParentDir(cfg.Database.DSN); err != nil {
          return nil, err
      }

      dsn := buildDSN(cfg.Database.DSN)
      db, err := sql.Open("sqlite", dsn)
      if err != nil {
          return nil, fmt.Errorf("sqlite.New: open: %w", err)
      }
      // Single writer + small pool: WAL allows concurrent readers but only one
      // writer at a time. Capping prevents busy errors under heavy ingest.
      db.SetMaxOpenConns(8)
      db.SetMaxIdleConns(4)
      db.SetConnMaxIdleTime(5 * time.Minute)

      // Verify PRAGMAs took effect (dsn-encoded pragmas are silently ignored on
      // unknown keys; failing fast at boot is friendlier than mysterious bugs).
      if err := verifyPragmas(db); err != nil {
          _ = db.Close()
          return nil, err
      }

      ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
      defer cancel()
      if err := db.PingContext(ctx); err != nil {
          _ = db.Close()
          return nil, fmt.Errorf("sqlite.New: ping: %w", err)
      }

      l.Info("sqlite db ready",
          zap.String("path", cfg.Database.DSN),
          zap.String("raw", cfg.Database.Raw),
      )

      s := &Store{db: db}
      lc.Append(fx.Hook{
          OnStop: func(_ context.Context) error {
              l.Info("closing sqlite db")
              return s.Close()
          },
      })
      return s, nil
  }

  // buildDSN appends pragma query params to the path so the driver applies them
  // on every connection in the pool (per-connection state).
  func buildDSN(path string) string {
      if path == ":memory:" || strings.HasPrefix(path, "file::memory:") {
          // In-memory test DBs still need pragmas, but use a shared cache so all
          // connections in the pool see the same database.
          q := pragmaQuery()
          return fmt.Sprintf("file::memory:?cache=shared&%s", q)
      }
      // Use a file: URL so the driver respects query params consistently.
      return fmt.Sprintf("file:%s?%s", path, pragmaQuery())
  }

  func pragmaQuery() string {
      parts := make([]string, 0, len(pragmas))
      for _, p := range pragmas {
          parts = append(parts, "_pragma="+p)
      }
      return strings.Join(parts, "&")
  }

  func verifyPragmas(db *sql.DB) error {
      checks := map[string]string{
          "journal_mode": "wal",
          "foreign_keys": "1",
      }
      for k, want := range checks {
          var got string
          if err := db.QueryRow("PRAGMA " + k).Scan(&got); err != nil {
              return fmt.Errorf("sqlite.New: read PRAGMA %s: %w", k, err)
          }
          if !strings.EqualFold(got, want) {
              return fmt.Errorf("sqlite.New: PRAGMA %s = %q, want %q", k, got, want)
          }
      }
      return nil
  }

  func ensureParentDir(path string) error {
      if path == ":memory:" || strings.HasPrefix(path, "file::memory:") {
          return nil
      }
      dir := filepath.Dir(path)
      if dir == "" || dir == "." {
          return nil
      }
      return os.MkdirAll(dir, 0o755)
  }
  ```

  Commit: `feat(storage): SQLite implementation with PRAGMAs + fx provider`.

- [ ] **Step 6 — `internal/storage/sqlite/sqlite_test.go`**

  Cover three things:
  1. Open against `:memory:`, ping returns nil.
  2. PRAGMAs are applied (`journal_mode=memory` for in-memory dbs is fine — assert `foreign_keys=1` is enough for in-memory; for `wal`, exercise a tempfile path in a separate test).
  3. Closing the store closes the pool.

  ```go
  package sqlite_test

  import (
      "context"
      "path/filepath"
      "testing"

      "github.com/karnstack/tempo/internal/config"
      "github.com/karnstack/tempo/internal/storage/sqlite"
      "go.uber.org/fx/fxtest"
      "go.uber.org/zap/zaptest"
  )

  func TestNew_InMemory(t *testing.T) {
      t.Parallel()
      lc := fxtest.NewLifecycle(t)
      l := zaptest.NewLogger(t)
      cfg := &config.Config{Database: config.Database{Driver: "sqlite", DSN: ":memory:", Raw: "sqlite://:memory:"}}

      s, err := sqlite.New(lc, l, cfg)
      if err != nil {
          t.Fatalf("New: %v", err)
      }
      if err := s.Ping(context.Background()); err != nil {
          t.Fatalf("Ping: %v", err)
      }
      // PRAGMA foreign_keys=1 is the in-memory invariant we care about.
      var fk int
      if err := s.DB().QueryRow("PRAGMA foreign_keys").Scan(&fk); err != nil {
          t.Fatalf("read PRAGMA foreign_keys: %v", err)
      }
      if fk != 1 {
          t.Fatalf("foreign_keys = %d, want 1", fk)
      }
      lc.RequireStart().RequireStop()
  }

  func TestNew_TempFile_WAL(t *testing.T) {
      t.Parallel()
      lc := fxtest.NewLifecycle(t)
      l := zaptest.NewLogger(t)
      path := filepath.Join(t.TempDir(), "tempo.db")
      cfg := &config.Config{Database: config.Database{Driver: "sqlite", DSN: path, Raw: "sqlite://" + path}}

      s, err := sqlite.New(lc, l, cfg)
      if err != nil {
          t.Fatalf("New: %v", err)
      }
      var mode string
      if err := s.DB().QueryRow("PRAGMA journal_mode").Scan(&mode); err != nil {
          t.Fatalf("read PRAGMA journal_mode: %v", err)
      }
      if mode != "wal" {
          t.Fatalf("journal_mode = %q, want wal", mode)
      }
      lc.RequireStart().RequireStop()
  }

  func TestNew_RejectsWrongDriver(t *testing.T) {
      t.Parallel()
      lc := fxtest.NewLifecycle(t)
      l := zaptest.NewLogger(t)
      cfg := &config.Config{Database: config.Database{Driver: "postgres", DSN: "postgres://x", Raw: "postgres://x"}}
      if _, err := sqlite.New(lc, l, cfg); err == nil {
          t.Fatal("expected error for non-sqlite driver")
      }
  }
  ```

  Commit: `test(storage): SQLite open + PRAGMA + lifecycle coverage`.

- [ ] **Step 7 — `internal/storage/postgres/postgres.go`** (stub)

  ```go
  // Package postgres is a placeholder for the v1.x Postgres backend. The seam
  // exists today so call sites can be written against storage.Storage; the real
  // implementation is added when Postgres parity is in scope.
  package postgres

  import (
      "errors"

      "github.com/karnstack/tempo/internal/storage"
  )

  // Open is a stub. Returns an error in v1.
  func Open() (storage.Storage, error) {
      return nil, errors.New("postgres backend not implemented in v1")
  }
  ```

  Commit: `feat(storage): postgres stub package`.

- [ ] **Step 8 — `cmd/migrate/main.go`** (goose-as-a-library, pure Go)

  ```go
  // Command migrate runs goose migrations against the configured TEMPO_DB.
  // It links modernc.org/sqlite directly so no CGo is required.
  package main

  import (
      "context"
      "database/sql"
      "fmt"
      "os"
      "path/filepath"

      "github.com/karnstack/tempo/internal/config"
      "github.com/karnstack/tempo/internal/logger"
      "github.com/pressly/goose/v3"
      "go.uber.org/zap"
      _ "modernc.org/sqlite"
  )

  const migrationsDir = "migrations"

  func main() {
      l := logger.NewStandalone()
      defer func() { _ = l.Sync() }()

      if len(os.Args) < 2 {
          l.Fatal("usage: migrate <up|down|status|version>")
      }
      cmd := os.Args[1]

      cfg := config.Load()
      if cfg.Database.Driver != "sqlite" {
          l.Fatal("migrate: only sqlite is supported in v1", zap.String("driver", cfg.Database.Driver))
      }

      if err := os.MkdirAll(filepath.Dir(cfg.Database.DSN), 0o755); err != nil {
          l.Fatal("migrate: ensure data dir", zap.Error(err))
      }

      db, err := sql.Open("sqlite", cfg.Database.DSN)
      if err != nil {
          l.Fatal("migrate: open db", zap.Error(err))
      }
      defer db.Close()

      goose.SetLogger(zapGooseLogger{l: l})
      if err := goose.SetDialect("sqlite3"); err != nil {
          l.Fatal("migrate: set dialect", zap.Error(err))
      }

      ctx := context.Background()
      switch cmd {
      case "up":
          err = goose.UpContext(ctx, db, migrationsDir)
      case "down":
          err = goose.DownContext(ctx, db, migrationsDir)
      case "status":
          err = goose.StatusContext(ctx, db, migrationsDir)
      case "version":
          err = goose.VersionContext(ctx, db, migrationsDir)
      default:
          l.Fatal("migrate: unknown command", zap.String("cmd", cmd))
      }
      if err != nil {
          l.Fatal("migrate: run", zap.String("cmd", cmd), zap.Error(err))
      }
      fmt.Println("migrate", cmd, "ok")
  }

  type zapGooseLogger struct{ l *zap.Logger }

  func (z zapGooseLogger) Fatalf(format string, v ...any) { z.l.Sugar().Fatalf(format, v...) }
  func (z zapGooseLogger) Printf(format string, v ...any) { z.l.Sugar().Infof(format, v...) }
  ```

  Commit: `feat(cmd/migrate): pure-Go goose runner`.

- [ ] **Step 9 — `sqlc.yaml`**

  ```yaml
  version: "2"
  sql:
    - engine: "sqlite"
      schema: "migrations"
      queries: "internal/storage/sqlite/queries"
      gen:
        go:
          package: "sqlitedb"
          out: "internal/storage/sqlite/sqlitedb"
          sql_package: "database/sql"
          emit_interface: true
          emit_json_tags: true
          emit_pointers_for_null_types: true
  ```

  Confirm `sqlc generate` runs without error against the empty queries dir. (sqlc may print a warning about no queries — that's fine; verify.sh checks the exit code, not stderr noise.)

  Add `internal/storage/sqlite/queries/.gitkeep` so the directory is tracked by git.

  Commit: `chore(storage): sqlc.yaml + empty queries dir`.

- [ ] **Step 10 — `migrations/.gitkeep` + `migrations/README.md`**

  ```
  # Migrations

  goose-managed SQL files. `make migrate-up` applies pending migrations against
  the database at `TEMPO_DB` (default `sqlite://./data/tempo.db`).
  ```

  Commit: `chore(migrations): seed dir`.

- [ ] **Step 11 — Update `Makefile`**

  Add (and slot near the existing build target):

  ```make
  migrate-up: ## Apply all pending DB migrations
  	go run ./cmd/migrate up

  migrate-down: ## Roll back the latest DB migration
  	go run ./cmd/migrate down

  migrate-status: ## Show migration status
  	go run ./cmd/migrate status

  sqlc-generate: ## Regenerate sqlc-typed query bindings
  	@command -v sqlc >/dev/null || (echo "install sqlc: mise install or 'go install github.com/sqlc-dev/sqlc/cmd/sqlc@v1.31.1'" && exit 1)
  	sqlc generate
  ```

  Add `migrate-up migrate-down migrate-status sqlc-generate` to the `.PHONY` line.

  Commit: `chore(make): migrate + sqlc-generate targets`.

- [ ] **Step 12 — Wire fx in `cmd/tempo/main.go`**

  ```go
  package main

  import (
      "context"

      "github.com/karnstack/tempo/internal/api"
      "github.com/karnstack/tempo/internal/config"
      "github.com/karnstack/tempo/internal/logger"
      "github.com/karnstack/tempo/internal/storage"
      "github.com/karnstack/tempo/internal/storage/sqlite"
      "go.uber.org/fx"
      "go.uber.org/fx/fxevent"
      "go.uber.org/zap"
  )

  func main() {
      fx.New(
          fx.Provide(
              logger.New,
              config.Load,
              sqlite.New,
          ),
          fx.Decorate(func(l *zap.Logger) *zap.Logger {
              return l.With(zap.String("service", "tempo"))
          }),
          fx.Invoke(api.Run),
          fx.Invoke(touchStorage),
          fx.WithLogger(func(l *zap.Logger) fxevent.Logger {
              return &fxevent.ZapLogger{Logger: l}
          }),
      ).Run()
  }

  // touchStorage forces fx to instantiate the Storage so the SQLite open + PRAGMA
  // checks run at boot. Real consumers (auth, ingest, rollup) replace this in 0016+.
  func touchStorage(s storage.Storage, l *zap.Logger) error {
      ctx, cancel := context.WithTimeout(context.Background(), 2_000_000_000) // 2s
      defer cancel()
      if err := s.Ping(ctx); err != nil {
          return err
      }
      l.Info("storage warmup ok")
      return nil
  }
  ```

  (Note: `2_000_000_000` is `2*time.Second`. Use `time.Second` import in the actual file. Inline numeric here just to keep the snippet short — write it idiomatically in code.)

  Commit: `feat(server): wire SQLite into fx graph + warmup ping`.

- [ ] **Step 13 — `go mod tidy`** to clean up `go.sum`.

- [ ] **Step 14 — Run `./verify.sh`** from the task dir.

- [ ] **Step 15 — Final commit if anything's left dangling.**

## Notes

- We deliberately do **not** install the `goose` CLI. Goose's CLI for SQLite needs `mattn/go-sqlite3` (CGo). Using goose as a library with `modernc.org/sqlite` keeps the toolchain pure-Go end-to-end. `make migrate-up` is `go run ./cmd/migrate up` — fast enough on subsequent runs, and zero install steps.
- `sqlc` is CLI-only (no library API). It's pinned in `.mise.toml` so `mise install` brings it in. Falling back to `go install github.com/sqlc-dev/sqlc/cmd/sqlc@v1.31.1` works too — the Makefile's `sqlc-generate` target prints both options on miss.
- The `Storage` interface deliberately does **not** embed `*sql.DB` — repo methods (Users, Connections, …) will hang off it directly in 0012. Today it exposes `DB()` as an escape hatch for migration code and ad-hoc queries.
- The dialect-aware UPSERT/`now()` shim mentioned in the spec lives in 0012 alongside sqlc. 0007 doesn't need it yet.
- `cfgx` adoption (mentioned in memory + plan task 0013) replaces the `os.Getenv`/`panic` pattern in `config.Load()` with typed config and explicit errors. 0007 keeps the existing minimal style — 0013 owns the upgrade.
