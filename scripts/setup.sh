#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
source scripts/env.sh
task_default_dsn="postgres://devfest26_${WORKSHOP_INSTANCE}@127.0.0.1:${PG_WORKSHOP_PORT}/devfest26_${WORKSHOP_INSTANCE}?sslmode=disable"
if [ -z "${DATABASE_URL:-}" ] && [ "$PAYMENT_DATABASE_URL" = "$task_default_dsn" ]; then
  bash scripts/postgres.sh
fi
./bin/devfest database-init
./bin/devfest migrate
./bin/devfest seed
