#!/usr/bin/env bash
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"

required=(.pre-commit-config.yaml)
for f in "${required[@]}"; do
  if [[ ! -s "$f" ]]; then
    echo "FAIL: missing or empty: $f" >&2
    exit 1
  fi
  echo "  $f ok"
done

if ! grep -q "^pre-commit-install:" Makefile; then
  echo "FAIL: Makefile missing pre-commit-install target" >&2
  exit 1
fi
echo "  Makefile target pre-commit-install present"

# YAML sanity via the gopkg.in/yaml.v3 one-shot (same pattern as 0057/0058).
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

abs="$PWD/.pre-commit-config.yaml"
if ! (cd "$tmpdir" && go run . "$abs" >/dev/null); then
  echo "FAIL: YAML parse error in .pre-commit-config.yaml" >&2
  exit 1
fi
echo "  .pre-commit-config.yaml parses"

echo "VERIFY OK"
