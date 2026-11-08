#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")"
if [ -x bin/gateway ]; then
  exec bin/gateway --demo
fi
exec ./start-local.sh --demo
