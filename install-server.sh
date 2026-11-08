#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")"
if ! command -v python3 >/dev/null 2>&1; then
  echo 'Python 3 ab Version 3.9 ist für die lokale Port- und Docker-Prüfung erforderlich.' >&2
  exit 1
fi
exec python3 scripts/install_server.py "$@"
