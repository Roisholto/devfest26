#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
source scripts/env.sh
mkdir -p ".local/$WORKSHOP_INSTANCE"
./bin/devfest preflight
python3 - <<'PY'
import os, socket
for key in ('API_ADDRESS', 'PAYMENT_ADDRESS'):
    host, port = os.environ[key].rsplit(':', 1)
    try:
        connection = socket.create_connection((host, int(port)), timeout=0.3)
    except ConnectionRefusedError:
        continue
    except OSError:
        continue
    else:
        connection.close()
        raise SystemExit(key + ' is already in use; stop that instance before starting another.')
PY
pids=()
cleanup() { for pid in "${pids[@]}"; do kill "$pid" 2>/dev/null || true; done; if [ -f ".local/$WORKSHOP_INSTANCE/worker.pid" ]; then kill "$(cat ".local/$WORKSHOP_INSTANCE/worker.pid")" 2>/dev/null || true; fi; }
trap cleanup EXIT INT TERM
for service in payment api worker; do
  ./bin/devfest "$service" > ".local/$WORKSHOP_INSTANCE/$service.log" 2>&1 &
  pids+=("$!")
  echo "$!" > ".local/$WORKSHOP_INSTANCE/$service.pid"
done
sleep 0.3
for pid in "${pids[@]}"; do
  if ! kill -0 "$pid" 2>/dev/null; then echo "Service exited; inspect .local/$WORKSHOP_INSTANCE/*.log" >&2; exit 1; fi
done
echo "Services started: instance=$WORKSHOP_INSTANCE mode=$MODEL_MODE API=http://$API_ADDRESS"
echo "Stop worker: kill -TERM \$(cat .local/$WORKSHOP_INSTANCE/worker.pid)"
echo "Restart worker: make worker (same environment/checkpoint)"
# The worker is intentionally stopped/restarted in the crash exercise. Keep the
# API and provider alive while it is down.
wait "${pids[0]}" "${pids[1]}"
