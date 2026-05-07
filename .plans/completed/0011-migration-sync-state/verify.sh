#!/usr/bin/env bash
set -euo pipefail

cd "$(git rev-parse --show-toplevel)"

TMP_DIR=$(mktemp -d)
trap 'rm -rf "$TMP_DIR"' EXIT

DB="$TMP_DIR/tempo-verify.db"
export TEMPO_DB="sqlite://$DB"

PRIOR_TABLES=(
  tenants users sessions gh_tokens connections repos gh_users
  commits pull_requests pr_reviews pr_review_comments pr_issue_comments deployments
  daily_engineer_stats daily_repo_stats daily_review_latency daily_review_load
)
EXPECTED_0004=(sync_runs sync_cursors)
EXPECTED_INDEXES=(sync_runs_connection_started_idx)

echo "==> migrate up (fresh — 0001..0004)"
go run ./cmd/migrate up

echo "==> verify 0004 tables exist"
for t in "${EXPECTED_0004[@]}"; do
  if ! sqlite3 "$DB" "SELECT name FROM sqlite_master WHERE type='table' AND name='$t';" | grep -qx "$t"; then
    echo "FAIL: table $t not created" >&2
    exit 1
  fi
  echo "  ok: $t"
done

echo "==> verify prior-migration tables still present"
for t in "${PRIOR_TABLES[@]}"; do
  if ! sqlite3 "$DB" "SELECT name FROM sqlite_master WHERE type='table' AND name='$t';" | grep -qx "$t"; then
    echo "FAIL: table $t (from prior migration) missing after 0004 up" >&2
    exit 1
  fi
done

echo "==> verify schema has NO foreign keys"
fk_count=$(sqlite3 "$DB" "SELECT COUNT(*) FROM sqlite_master m, pragma_foreign_key_list(m.name) WHERE m.type='table';")
if [[ "$fk_count" != "0" ]]; then
  echo "FAIL: schema declares $fk_count foreign keys" >&2
  exit 1
fi

echo "==> verify schema has NO CHECK constraints"
check_count=$(sqlite3 "$DB" "SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND sql LIKE '%CHECK%';")
if [[ "$check_count" != "0" ]]; then
  echo "FAIL: schema declares CHECK constraints" >&2
  exit 1
fi

echo "==> verify expected indices exist"
for idx in "${EXPECTED_INDEXES[@]}"; do
  if ! sqlite3 "$DB" "SELECT name FROM sqlite_master WHERE type='index' AND name='$idx';" | grep -qx "$idx"; then
    echo "FAIL: index $idx not created" >&2
    exit 1
  fi
  echo "  ok: $idx"
done

echo "==> migrate down (one step — drops only 0004)"
go run ./cmd/migrate down

echo "==> verify 0004 tables removed"
for t in "${EXPECTED_0004[@]}"; do
  if sqlite3 "$DB" "SELECT name FROM sqlite_master WHERE type='table' AND name='$t';" | grep -qx "$t"; then
    echo "FAIL: table $t still exists after down" >&2
    exit 1
  fi
done
echo "  ok: 0004 tables gone"

echo "==> verify prior tables NOT touched by single down step"
for t in "${PRIOR_TABLES[@]}"; do
  if ! sqlite3 "$DB" "SELECT name FROM sqlite_master WHERE type='table' AND name='$t';" | grep -qx "$t"; then
    echo "FAIL: down step removed prior-migration table $t" >&2
    exit 1
  fi
done
echo "  ok: prior tables intact"

echo "==> migrate up again (idempotency)"
go run ./cmd/migrate up
for t in "${EXPECTED_0004[@]}"; do
  if ! sqlite3 "$DB" "SELECT name FROM sqlite_master WHERE type='table' AND name='$t';" | grep -qx "$t"; then
    echo "FAIL: re-up did not recreate $t" >&2
    exit 1
  fi
done
echo "  ok: re-up recreated all 0004 tables"

echo "==> storage package tests still pass"
go test ./internal/storage/...

echo "VERIFY OK"
