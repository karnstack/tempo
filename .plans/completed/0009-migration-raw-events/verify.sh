#!/usr/bin/env bash
set -euo pipefail

cd "$(git rev-parse --show-toplevel)"

TMP_DIR=$(mktemp -d)
trap 'rm -rf "$TMP_DIR"' EXIT

DB="$TMP_DIR/tempo-verify.db"
export TEMPO_DB="sqlite://$DB"

EXPECTED_0001=(tenants users sessions gh_tokens connections repos gh_users)
EXPECTED_0002=(commits pull_requests pr_reviews pr_review_comments pr_issue_comments deployments)

EXPECTED_INDEXES=(
  commits_repo_authored_idx
  commits_author_authored_idx
  pull_requests_repo_gh_uniq
  pull_requests_author_created_idx
  pull_requests_repo_merged_idx
  pull_requests_repo_state_idx
  pr_reviews_pr_idx
  pr_reviews_reviewer_submitted_idx
  pr_review_comments_pr_idx
  pr_review_comments_author_created_idx
  pr_issue_comments_pr_idx
  pr_issue_comments_author_created_idx
  deployments_repo_created_idx
  deployments_repo_env_idx
)

echo "==> migrate up (fresh — applies 0001 and 0002)"
go run ./cmd/migrate up

echo "==> verify all 0002 tables exist"
for t in "${EXPECTED_0002[@]}"; do
  if ! sqlite3 "$DB" "SELECT name FROM sqlite_master WHERE type='table' AND name='$t';" | grep -qx "$t"; then
    echo "FAIL: table $t not created" >&2
    exit 1
  fi
  echo "  ok: $t"
done

echo "==> verify 0001 tables also still present (sanity)"
for t in "${EXPECTED_0001[@]}"; do
  if ! sqlite3 "$DB" "SELECT name FROM sqlite_master WHERE type='table' AND name='$t';" | grep -qx "$t"; then
    echo "FAIL: table $t (from 0001) missing after 0002 up" >&2
    exit 1
  fi
done

echo "==> verify schema has NO foreign keys (project rule: enforce in Go, not SQL)"
fk_count=$(sqlite3 "$DB" "SELECT COUNT(*) FROM sqlite_master m, pragma_foreign_key_list(m.name) WHERE m.type='table';")
if [[ "$fk_count" != "0" ]]; then
  echo "FAIL: schema declares $fk_count foreign keys" >&2
  sqlite3 "$DB" "SELECT m.name, fk.* FROM sqlite_master m, pragma_foreign_key_list(m.name) fk WHERE m.type='table';" >&2
  exit 1
fi

echo "==> verify schema has NO CHECK constraints"
check_count=$(sqlite3 "$DB" "SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND sql LIKE '%CHECK%';")
if [[ "$check_count" != "0" ]]; then
  echo "FAIL: schema declares CHECK constraints" >&2
  sqlite3 "$DB" "SELECT name, sql FROM sqlite_master WHERE type='table' AND sql LIKE '%CHECK%';" >&2
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

echo "==> migrate down (one step — drops only 0002)"
go run ./cmd/migrate down

echo "==> verify 0002 tables removed"
for t in "${EXPECTED_0002[@]}"; do
  if sqlite3 "$DB" "SELECT name FROM sqlite_master WHERE type='table' AND name='$t';" | grep -qx "$t"; then
    echo "FAIL: table $t still exists after down" >&2
    exit 1
  fi
done
echo "  ok: 0002 tables gone"

echo "==> verify 0001 tables NOT touched by single down step"
for t in "${EXPECTED_0001[@]}"; do
  if ! sqlite3 "$DB" "SELECT name FROM sqlite_master WHERE type='table' AND name='$t';" | grep -qx "$t"; then
    echo "FAIL: down step removed 0001 table $t (should only roll back 0002)" >&2
    exit 1
  fi
done
echo "  ok: 0001 tables intact"

echo "==> migrate up again (idempotency)"
go run ./cmd/migrate up
for t in "${EXPECTED_0002[@]}"; do
  if ! sqlite3 "$DB" "SELECT name FROM sqlite_master WHERE type='table' AND name='$t';" | grep -qx "$t"; then
    echo "FAIL: re-up did not recreate $t" >&2
    exit 1
  fi
done
echo "  ok: re-up recreated all 0002 tables"

echo "==> storage package tests still pass"
go test ./internal/storage/...

echo "VERIFY OK"
