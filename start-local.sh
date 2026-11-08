#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")"
export GOPATH="${GOPATH:-$PWD/.cache/go}"
export GOMODCACHE="${GOMODCACHE:-$PWD/.cache/go-mod}"
export GOCACHE="${GOCACHE:-$PWD/.cache/go-build}"
if ! command -v go >/dev/null || ! command -v npm >/dev/null; then
  echo 'Go mit automatischer Toolchain-Auswahl und Node >=22.12 mit npm sind erforderlich (Build mit Go >=1.27.3).' >&2; exit 1
fi
if [ ! -d web/node_modules ]; then (cd web && npm ci); fi
(cd web && npm run build)
mkdir -p bin
go build -trimpath -o bin/gateway ./cmd/gateway
exec bin/gateway "$@"
