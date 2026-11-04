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
export WORKSHOP_INSTANCE="${WORKSHOP_INSTANCE:-participant}"
case "$WORKSHOP_INSTANCE" in presenter|participant) ;; *) echo 'Choose presenter or participant' >&2; return 1 2>/dev/null || exit 1;; esac
export WORKSHOP_CHECKPOINT="${WORKSHOP_CHECKPOINT:-$(git branch --show-current | tr '/' '-')}"
export MODEL_MODE="${MODEL_MODE:-scripted}"
export GEMINI_MODEL="${GEMINI_MODEL:-gemini-3.6-flash}"
export PG_WORKSHOP_PORT="${PG_WORKSHOP_PORT:-55432}"
export PAYMENT_DATABASE_URL="${DATABASE_URL:-${PAYMENT_DATABASE_URL:-postgres://devfest26_${WORKSHOP_INSTANCE}@127.0.0.1:${PG_WORKSHOP_PORT}/devfest26_${WORKSHOP_INSTANCE}?sslmode=disable}}"
if [ "$WORKSHOP_INSTANCE" = presenter ]; then
  export API_ADDRESS="${API_ADDRESS:-127.0.0.1:8080}" PAYMENT_ADDRESS="${PAYMENT_ADDRESS:-127.0.0.1:8081}"
else
  export API_ADDRESS="${API_ADDRESS:-127.0.0.1:8090}" PAYMENT_ADDRESS="${PAYMENT_ADDRESS:-127.0.0.1:8091}"
fi
export MODEL_AUDIT_PATH="${MODEL_AUDIT_PATH:-.local/${WORKSHOP_INSTANCE}/model-invocations.jsonl}"
