#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
source scripts/env.sh
mkdir -p ".local/$WORKSHOP_INSTANCE"
./bin/devfest preflight
pids=()
cleanup() { for pid in "${pids[@]}"; do kill "$pid" 2>/dev/null || true; done; if [ -f ".local/$WORKSHOP_INSTANCE/worker.pid" ]; then kill "$(cat ".local/$WORKSHOP_INSTANCE/worker.pid")" 2>/dev/null || true; fi; }
trap cleanup EXIT INT TERM
for service in payment api worker; do
  ./bin/devfest "$service" > ".local/$WORKSHOP_INSTANCE/$service.log" 2>&1 &
  pids+=("$!")
  echo "$!" > ".local/$WORKSHOP_INSTANCE/$service.pid"
done
echo "Services started: instance=$WORKSHOP_INSTANCE mode=$MODEL_MODE API=http://$API_ADDRESS"
echo "Stop worker: kill -TERM \$(cat .local/$WORKSHOP_INSTANCE/worker.pid)"
echo "Restart worker: make worker (same environment/checkpoint)"
# The worker is intentionally stopped/restarted in the crash exercise. Keep the
# API and provider alive while it is down.
wait "${pids[0]}" "${pids[1]}"
