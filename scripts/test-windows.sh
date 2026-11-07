#!/usr/bin/env bash
# Compile les tests pour Windows et les exécute sous Windows, depuis WSL
# (interopérabilité WSL). Contrôle réel de la portabilité (D-16) : verrou,
# remplacement de fichier, chemins. pandoc n'étant en général pas installé
# côté Windows, les tests qui en dépendent sont sautés.
set -euo pipefail
cd "$(dirname "$0")/.."
command -v go >/dev/null 2>&1 || export PATH="$HOME/.local/go/bin:$PATH"

if ! command -v cmd.exe >/dev/null 2>&1; then
  echo "Pas d'interopérabilité Windows (hors WSL) : rien à faire."; exit 0
fi
out=$(mktemp -d)
trap 'rm -rf "$out"' EXIT
status=0
for pkg in $(go list -f '{{if .TestGoFiles}}{{.ImportPath}}{{end}}' ./...); do
  name=$(basename "$pkg")
  GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go test -c -o "$out/$name.test.exe" "$pkg"
  dir=$(go list -f '{{.Dir}}' "$pkg")
  echo "== $pkg (Windows)"
  if ! (cd "$dir" && "$out/$name.test.exe" -test.count=1 2>&1 | tr -d '\r'); then
    status=1
  fi
done
exit $status
