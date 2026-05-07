#!/usr/bin/env bash
set -euo pipefail

cd "$(git rev-parse --show-toplevel)"

TMP_DIR=$(mktemp -d)
trap 'rm -rf "$TMP_DIR"' EXIT

DB="$TMP_DIR/tempo-verify.db"
export TEMPO_DB="sqlite://$DB"

EXPECTED=(tenants users sessions gh_tokens connections repos gh_users)

echo "==> migrate up (fresh)"
go run ./cmd/migrate up

echo "==> verify all expected tables exist"
for t in "${EXPECTED[@]}"; do
  if ! sqlite3 "$DB" "SELECT name FROM sqlite_master WHERE type='table' AND name='$t';" | grep -qx "$t"; then
    echo "FAIL: table $t not created" >&2
    exit 1
  fi
  echo "  ok: $t"
done

echo "==> verify schema has NO foreign keys (project rule: enforce in Go, not SQL)"
fk_count=$(sqlite3 "$DB" "SELECT COUNT(*) FROM sqlite_master m, pragma_foreign_key_list(m.name) WHERE m.type='table';")
if [[ "$fk_count" != "0" ]]; then
  echo "FAIL: schema declares $fk_count foreign keys; this project enforces refs in Go, not SQL" >&2
  sqlite3 "$DB" "SELECT m.name AS tbl, fk.* FROM sqlite_master m, pragma_foreign_key_list(m.name) fk WHERE m.type='table';" >&2
  exit 1
fi

echo "==> verify schema has NO CHECK constraints (project rule: enum validation in Go)"
check_count=$(sqlite3 "$DB" "SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND sql LIKE '%CHECK%';")
if [[ "$check_count" != "0" ]]; then
  echo "FAIL: schema declares CHECK constraints" >&2
  sqlite3 "$DB" "SELECT name, sql FROM sqlite_master WHERE type='table' AND sql LIKE '%CHECK%';" >&2
  exit 1
fi

echo "==> verify expected unique/partial indices exist"
EXPECTED_INDEXES=(
  users_tenant_email_uniq
  sessions_user_idx
  gh_tokens_tenant_idx
  connections_repo_uniq
  connections_org_uniq
  repos_tenant_gh_uniq
  gh_users_tenant_gh_uniq
)
for idx in "${EXPECTED_INDEXES[@]}"; do
  if ! sqlite3 "$DB" "SELECT name FROM sqlite_master WHERE type='index' AND name='$idx';" | grep -qx "$idx"; then
    echo "FAIL: index $idx not created" >&2
    exit 1
  fi
  echo "  ok: $idx"
done

echo "==> migrate down"
go run ./cmd/migrate down

echo "==> verify tables removed (goose_db_version should be the only remnant)"
remaining=$(sqlite3 "$DB" "SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%' AND name NOT LIKE 'goose_%';")
if [[ -n "$remaining" ]]; then
  echo "FAIL: tables still exist after down: $remaining" >&2
  exit 1
fi
echo "  ok: no app tables remain"

echo "==> migrate up again (idempotency / repeatability)"
go run ./cmd/migrate up
for t in "${EXPECTED[@]}"; do
  if ! sqlite3 "$DB" "SELECT name FROM sqlite_master WHERE type='table' AND name='$t';" | grep -qx "$t"; then
    echo "FAIL: re-up did not recreate $t" >&2
    exit 1
  fi
done
echo "  ok: re-up recreated all tables"

echo "==> storage package tests still pass"
go test ./internal/storage/...

echo "VERIFY OK"
