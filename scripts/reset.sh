#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
source scripts/env.sh
if [ "${1:-}" != "--confirm" ]; then echo 'Stop services and close active workflows first. Use make reset CONFIRM=--confirm to reset ONLY the selected managed demo database.' >&2; exit 1; fi
expected="postgres://devfest26_${WORKSHOP_INSTANCE}@127.0.0.1:${PG_WORKSHOP_PORT}/devfest26_${WORKSHOP_INSTANCE}?sslmode=disable"
if [ "$PAYMENT_DATABASE_URL" != "$expected" ]; then echo 'Refusing reset of an externally configured database.' >&2; exit 1; fi
psql "$PAYMENT_DATABASE_URL" -v ON_ERROR_STOP=1 -c 'TRUNCATE receipt_attempts, receipts, debit_journal, transfers, beneficiary_operations, beneficiaries, accounts CASCADE'
./bin/payment seed
echo 'Payment data reset. Existing Temporal history and model audit logs are preserved; use new request keys.'
