#!/usr/bin/env bash
# Source this file; never overwrite HOME or other system configuration.
export GOTOOLCHAIN=go1.27.3
# Use the same dotenv parser as the Go binaries. NUL-delimited values are
# exported as data, so quotes, $, &, and command substitutions are not executed.
if [ -f .env ]; then
  task_dotenv_status=1
  while IFS= read -r -d '' task_dotenv_key && IFS= read -r -d '' task_dotenv_value; do
    if [ -z "$task_dotenv_key" ]; then task_dotenv_status="$task_dotenv_value"; break; fi
    export "$task_dotenv_key=$task_dotenv_value"
  done < <(if go run ./cmd/env; then printf '\0%s\0' 0; else printf '\0%s\0' 1; fi)
  if [ "$task_dotenv_status" != 0 ]; then return 1 2>/dev/null || exit 1; fi
  unset task_dotenv_status task_dotenv_key task_dotenv_value
fi
# Verification helpers reuse the explicitly configured database URL.
export PAYMENT_DATABASE_URL="${DATABASE_URL:-${PAYMENT_DATABASE_URL:-}}"
if [ -z "$PAYMENT_DATABASE_URL" ]; then
  echo 'DATABASE_URL is required in .env or the environment.' >&2
  return 1 2>/dev/null || exit 1
fi
