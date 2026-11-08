#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")"
export GOPATH="${GOPATH:-$PWD/.cache/go}"
export GOMODCACHE="${GOMODCACHE:-$PWD/.cache/go-mod}"
export GOCACHE="${GOCACHE:-$PWD/.cache/go-build}"
if [ ! -d web/node_modules ]; then (cd web && npm ci); fi
(cd web && npm run build)
python3 -m unittest discover -s tests -p 'test_*.py'
go test -race ./...
mkdir -p bin
go build -trimpath -o bin/gateway ./cmd/gateway
