#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
source scripts/env.sh
pgbin="${PG_BIN:-/usr/lib/postgresql/17/bin}"
cluster="$PWD/.local/postgres"
mkdir -p .local
if [ ! -x "$pgbin/initdb" ]; then echo 'Install PostgreSQL 17 or set PG_BIN.' >&2; exit 1; fi
if [ ! -f "$cluster/PG_VERSION" ]; then
  "$pgbin/initdb" -D "$cluster" -U workshop_admin --auth-local=trust --auth-host=trust --no-locale -E UTF8 > .local/postgres-init.log
fi
if ! "$pgbin/pg_ctl" -D "$cluster" status >/dev/null 2>&1; then
  "$pgbin/pg_ctl" -D "$cluster" -l "$PWD/.local/postgres.log" -o "-h 127.0.0.1 -p $PG_WORKSHOP_PORT -k $PWD/.local" start
fi
# This script connects ONLY to the managed local workshop cluster.
for instance in presenter participant; do
  role="devfest26_${instance}"
  if ! "$pgbin/psql" -h 127.0.0.1 -p "$PG_WORKSHOP_PORT" -U workshop_admin -d postgres -Atc "SELECT 1 FROM pg_roles WHERE rolname='$role'" | rg -q 1; then
    "$pgbin/psql" -h 127.0.0.1 -p "$PG_WORKSHOP_PORT" -U workshop_admin -d postgres -v ON_ERROR_STOP=1 -c "CREATE ROLE $role LOGIN"
  fi
  if ! "$pgbin/psql" -h 127.0.0.1 -p "$PG_WORKSHOP_PORT" -U workshop_admin -d postgres -Atc "SELECT 1 FROM pg_database WHERE datname='$role'" | rg -q 1; then
    "$pgbin/createdb" -h 127.0.0.1 -p "$PG_WORKSHOP_PORT" -U workshop_admin -O "$role" "$role"
  fi
done
