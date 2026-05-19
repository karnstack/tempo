#!/usr/bin/env bash
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"

required=(.github/workflows/ci.yml)
for f in "${required[@]}"; do
  if [[ ! -s "$f" ]]; then
    echo "FAIL: missing or empty: $f" >&2
    exit 1
  fi
  echo "  $f ok"
done

# YAML sanity via a one-shot Go program using gopkg.in/yaml.v3 (in the
# module graph via kin-openapi). Same pattern as 0057's verify.sh.
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

abs="$PWD/.github/workflows/ci.yml"
if ! (cd "$tmpdir" && go run . "$abs" >/dev/null); then
  echo "FAIL: YAML parse error in ci.yml" >&2
  exit 1
fi
echo "  .github/workflows/ci.yml parses"

if command -v actionlint >/dev/null 2>&1; then
  actionlint .github/workflows/ci.yml
  echo "  actionlint ok"
else
  echo "  WARN: actionlint not available; skipping"
fi

echo "VERIFY OK"
