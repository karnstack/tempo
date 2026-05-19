#!/usr/bin/env bash
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"

required=(release-please-config.json .release-please-manifest.json .github/workflows/release-please.yml)
for f in "${required[@]}"; do
  if [[ ! -s "$f" ]]; then
    echo "FAIL: missing or empty: $f" >&2
    exit 1
  fi
  echo "  $f ok"
done

# JSON parse via python.
for j in release-please-config.json .release-please-manifest.json; do
  python3 -c "import json; json.load(open('$j'))" \
    || { echo "FAIL: JSON parse error in $j" >&2; exit 1; }
  echo "  $j parses"
done

# YAML parse via the gopkg.in/yaml.v3 one-shot.
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

abs="$PWD/.github/workflows/release-please.yml"
if ! (cd "$tmpdir" && go run . "$abs" >/dev/null); then
  echo "FAIL: YAML parse error in release-please.yml" >&2
  exit 1
fi
echo "  .github/workflows/release-please.yml parses"

echo "VERIFY OK"
