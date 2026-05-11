#!/usr/bin/env bash
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"

required=(
  .github/ISSUE_TEMPLATE/config.yml
  .github/ISSUE_TEMPLATE/bug_report.yml
  .github/ISSUE_TEMPLATE/feature_request.yml
  .github/PULL_REQUEST_TEMPLATE.md
  .github/CODEOWNERS
)

for f in "${required[@]}"; do
  if [[ ! -s "$f" ]]; then
    echo "FAIL: missing or empty: $f" >&2
    exit 1
  fi
  echo "  $f ok"
done

if command -v python3 >/dev/null; then
  for y in .github/ISSUE_TEMPLATE/*.yml; do
    python3 -c "import sys, yaml; yaml.safe_load(open('$y'))" || {
      echo "FAIL: YAML parse error in $y" >&2
      exit 1
    }
    echo "  $y parses"
  done
else
  echo "  WARN: python3 not found; skipping YAML parse check"
fi

echo "VERIFY OK"
