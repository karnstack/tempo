#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/../../.."

fail() { echo "VERIFY FAIL: $1" >&2; exit 1; }

# 1. Deps & tooling pinned
grep -q 'modernc.org/sqlite v1.50.0' go.mod || fail "modernc.org/sqlite v1.50.0 missing from go.mod"
grep -q 'github.com/pressly/goose/v3 v3.27.1' go.mod || fail "goose v3.27.1 missing from go.mod"
grep -q '^sqlc = "1.31.1"' .mise.toml || fail ".mise.toml must pin sqlc = \"1.31.1\""

# 2. Files exist
[ -f internal/storage/storage.go ] || fail "internal/storage/storage.go missing"
[ -f internal/storage/sqlite/sqlite.go ] || fail "internal/storage/sqlite/sqlite.go missing"
[ -f internal/storage/sqlite/sqlite_test.go ] || fail "internal/storage/sqlite/sqlite_test.go missing"
[ -f internal/storage/postgres/postgres.go ] || fail "internal/storage/postgres/postgres.go missing"
[ -f internal/storage/sqlite/queries/.gitkeep ] || fail "queries dir not seeded"
[ -f cmd/migrate/main.go ] || fail "cmd/migrate/main.go missing"
[ -f sqlc.yaml ] || fail "sqlc.yaml missing"
[ -f migrations/.gitkeep ] || fail "migrations/.gitkeep missing"
[ -f migrations/README.md ] || fail "migrations/README.md missing"

# 3. Storage interface signature
grep -q 'type Storage interface' internal/storage/storage.go || fail "Storage interface not declared"
grep -q 'DB() \*sql.DB' internal/storage/storage.go || fail "Storage.DB() *sql.DB missing"
grep -q 'Ping(ctx context.Context) error' internal/storage/storage.go || fail "Storage.Ping missing"
grep -q 'Close() error' internal/storage/storage.go || fail "Storage.Close missing"

# 4. Config parses TEMPO_DB
grep -q 'Database Database' internal/config/config.go || fail "config.Config missing Database field"
grep -q 'parseDB' internal/config/config.go || fail "parseDB helper missing"

# 5. Makefile targets
for tgt in migrate-up migrate-down migrate-status sqlc-generate; do
  grep -qE "^${tgt}:" Makefile || fail "Makefile missing target ${tgt}"
done

# 6. Build everything
go build ./... >/dev/null

# 7. Storage tests
go test ./internal/storage/... >/dev/null

# 8. Config tests
go test ./internal/config/... >/dev/null

# 9. cmd/migrate compiles
go build -o /tmp/tempo-migrate ./cmd/migrate >/dev/null
[ -x /tmp/tempo-migrate ] || fail "cmd/migrate did not produce a binary"
rm -f /tmp/tempo-migrate

# 10. End-to-end: spin up the binary against a temp SQLite db, hit /system/health, shut down.
make build >/dev/null
[ -x ./tempo ] || fail "./tempo not built"

TMPDIR=$(mktemp -d)
DB_PATH="$TMPDIR/tempo.db"
PORT=18${RANDOM:0:3}

TEMPO_LISTEN=":$PORT" TEMPO_DB="sqlite://$DB_PATH" ./tempo &
PID=$!
trap 'kill $PID 2>/dev/null || true; rm -rf "$TMPDIR"' EXIT

for _ in $(seq 1 50); do
  if curl -fsS "http://127.0.0.1:$PORT/api/v1/system/health" >/dev/null 2>&1; then
    break
  fi
  sleep 0.1
done

HEALTH=$(curl -fsS "http://127.0.0.1:$PORT/api/v1/system/health")
echo "$HEALTH" | grep -q '"status":"ok"' || fail "health not ok with sqlite wired: $HEALTH"

[ -f "$DB_PATH" ] || fail "sqlite file not created at $DB_PATH"

kill $PID 2>/dev/null || true
wait $PID 2>/dev/null || true

# 11. cmd/migrate compile check is in step 9 above. Runtime exercise of
#     migrate-up moves to 0008's verify.sh, where actual .sql files exist —
#     goose 3.27.1 errors on an empty migrations directory.

# 12. sqlc.yaml is well-formed. sqlc generate against an empty queries dir
#     fails by design — that gate moves to 0012's verify.sh once .sql files
#     land. Here we only confirm the binary is on PATH if mise installed it.
if command -v sqlc >/dev/null; then
  sqlc version >/dev/null
fi

echo "verify ok"
