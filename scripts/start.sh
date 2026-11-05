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
cleanup() {
  for pid in "${pids[@]}"; do kill "$pid" 2>/dev/null || true; done
  # A manually restarted agent is no longer a child of this launcher.
  local pid_file=".local/$WORKSHOP_INSTANCE/agent.pid"
  if [ -f "$pid_file" ]; then
    local agent_pid
    read -r agent_pid < "$pid_file"
    if [[ "$agent_pid" =~ ^[0-9]+$ ]] && [ -r "/proc/$agent_pid/cmdline" ]; then
      local -a agent_command
      mapfile -d '' -t agent_command < "/proc/$agent_pid/cmdline"
      if [ "${agent_command[0]##*/}" = devfest ] && [ "${agent_command[1]:-}" = agent ]; then
        kill -TERM "$agent_pid" 2>/dev/null || true
      fi
    fi
  fi
}
trap cleanup EXIT INT TERM
for service in payment agent; do
  ./bin/devfest "$service" > ".local/$WORKSHOP_INSTANCE/$service.log" 2>&1 &
  pids+=("$!")
  echo "$!" > ".local/$WORKSHOP_INSTANCE/$service.pid"
done
sleep 0.3
for pid in "${pids[@]}"; do
  if ! kill -0 "$pid" 2>/dev/null; then echo "Service exited; inspect .local/$WORKSHOP_INSTANCE/*.log" >&2; exit 1; fi
done
echo "Services started: instance=$WORKSHOP_INSTANCE mode=$MODEL_MODE API=http://$API_ADDRESS"
echo 'Restart the agent (API + worker): make restart-agent'
# The agent is intentionally restarted in the recovery demonstration. Keep
# the independent provider alive while its API and worker are unavailable.
wait "${pids[0]}"
