#!/usr/bin/env bash
set -euo pipefail

cd "$(git rev-parse --show-toplevel)"

echo "==> sqlc generate (idempotency check — committed bindings must match queries)"
make sqlc-generate >/dev/null
if ! git diff --quiet -- internal/storage/sqlite/sqlitedb; then
  echo "FAIL: sqlc generate produced a diff — bindings out of sync with queries" >&2
  git --no-pager diff -- internal/storage/sqlite/sqlitedb >&2
  exit 1
fi
echo "  ok: sqlc bindings up-to-date"

EXPECTED_QUERIES=(
  tenants users sessions gh_tokens connections repos gh_users
  commits pull_requests pr_reviews pr_review_comments pr_issue_comments deployments
  daily_engineer_stats daily_repo_stats daily_review_latency daily_review_load
  sync_runs sync_cursors
)

echo "==> all 19 query files present"
for q in "${EXPECTED_QUERIES[@]}"; do
  if [[ ! -f "internal/storage/sqlite/queries/$q.sql" ]]; then
    echo "FAIL: missing query file $q.sql" >&2
    exit 1
  fi
done
echo "  ok"

echo "==> all 19 generated bindings present"
for q in "${EXPECTED_QUERIES[@]}"; do
  if [[ ! -f "internal/storage/sqlite/sqlitedb/$q.sql.go" ]]; then
    echo "FAIL: missing binding $q.sql.go" >&2
    exit 1
  fi
done
echo "  ok"

echo "==> migrations Go package present and embedded"
if [[ ! -f "migrations/migrations.go" ]]; then
  echo "FAIL: migrations/migrations.go missing" >&2
  exit 1
fi
if ! grep -q "//go:embed \*.sql" migrations/migrations.go; then
  echo "FAIL: migrations.go missing //go:embed directive" >&2
  exit 1
fi
echo "  ok"

echo "==> go build ./..."
go build ./...
echo "  ok"

echo "==> go test ./..."
go test ./...
echo "  ok"

echo "==> migrate up against fresh DB (smoke — embedded FS path)"
TMP_DIR=$(mktemp -d)
trap 'rm -rf "$TMP_DIR"' EXIT
DB="$TMP_DIR/tempo-verify.db"
TEMPO_DB="sqlite://$DB" go run ./cmd/migrate up >/dev/null
for t in "${EXPECTED_QUERIES[@]}"; do
  if ! sqlite3 "$DB" "SELECT name FROM sqlite_master WHERE type='table' AND name='$t';" | grep -qx "$t"; then
    echo "FAIL: table $t missing after migrate up" >&2
    exit 1
  fi
done
echo "  ok: all 19 tables created from embedded migrations"

echo "VERIFY OK"
