#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
mkdir -p .local
exec temporal server start-dev --db-filename "$PWD/.local/temporal.db" --ip 127.0.0.1 --ui-ip 127.0.0.1
