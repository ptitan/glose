#!/usr/bin/env bash
# Construit l'exécutable dans bin/glose, avec la version tirée de Git.
set -euo pipefail
cd "$(dirname "$0")/.."
command -v go >/dev/null 2>&1 || export PATH="$HOME/.local/go/bin:$PATH"

version=$(git describe --tags --always --dirty 2>/dev/null || echo "0.0.0-dev")
mkdir -p bin
go build -ldflags "-X github.com/ptitan/glose/internal/cli.Version=$version" -o bin/glose ./cmd/glose
echo "bin/glose ($version)"
