#!/usr/bin/env bash
# Essais navigateur reproductibles, sans paquet npm ni téléchargement.
set -euo pipefail
cd "$(dirname "$0")/.."
if ! command -v node >/dev/null 2>&1; then
  echo "Node.js 22+ requis pour les essais navigateur (pas pour utiliser Glose)." >&2
  exit 1
fi
./scripts/build.sh
node scripts/browser/run.cjs
node scripts/browser/firefox.cjs
