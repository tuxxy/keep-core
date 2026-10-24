#!/usr/bin/env bash
# K-09a only. The final mutation campaign has no entry point here.
set -euo pipefail
root=$(cd "$(dirname "$0")/.." && pwd)
exec python3 "$root/test/frost/k09-local.py" "$@"
