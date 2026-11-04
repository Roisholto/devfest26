#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
source scripts/env.sh
mkdir -p ".local/$WORKSHOP_INSTANCE"
echo "$$" > ".local/$WORKSHOP_INSTANCE/worker.pid"
exec ./bin/devfest worker
