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

# YAML sanity check via a tiny Go program using gopkg.in/yaml.v3 (already
# in the module graph via kin-openapi).
tmpdir="$(mktemp -d)"
trap 'rm -rf "$tmpdir"' EXIT
cat >"$tmpdir/main.go" <<'EOG'
package main

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

func main() {
	if len(os.Args) < 2 {
		os.Exit(2)
	}
	b, err := os.ReadFile(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	var v any
	if err := yaml.Unmarshal(b, &v); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
EOG
cp go.mod go.sum "$tmpdir/" 2>/dev/null || true

for y in .github/ISSUE_TEMPLATE/*.yml; do
  abs="$PWD/$y"
  if ! (cd "$tmpdir" && go run . "$abs" >/dev/null); then
    echo "FAIL: YAML parse error in $y" >&2
    exit 1
  fi
  echo "  $y parses"
done

echo "VERIFY OK"
