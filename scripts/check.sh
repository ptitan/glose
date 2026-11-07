#!/usr/bin/env bash
# Vérifications avant de rendre la main :
# format, analyse statique, tests, compilation Windows de contrôle (D-16).
set -euo pipefail
cd "$(dirname "$0")/.."
command -v go >/dev/null 2>&1 || export PATH="$HOME/.local/go/bin:$PATH"

echo "== gofmt"
unformatted=$(gofmt -l .)
if [ -n "$unformatted" ]; then
  echo "Fichiers à formater (gofmt -w) :"; echo "$unformatted"; exit 1
fi

echo "== go vet"
go vet ./...

echo "== go test"
go test ./...

if command -v node >/dev/null 2>&1; then
  echo "== tests JavaScript de l'interface (sans dépendance)"
  node --test scripts/*.test.cjs
else
  echo "Tests JavaScript non exécutés : Node.js absent (facultatif, hors exécution de Glose)."
fi

echo "== compilation Windows de contrôle"
out=$(mktemp -d)
trap 'rm -rf "$out"' EXIT
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -o "$out/glose.exe" ./cmd/glose

if [ "${GLOSE_BROWSER_TESTS:-0}" = "1" ]; then
  echo "== tests navigateur"
  ./scripts/test-browser.sh
fi

echo "Tout est en ordre."
