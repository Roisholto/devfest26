#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
source scripts/env.sh
mkdir -p ".local/$WORKSHOP_INSTANCE"
pid_file=".local/$WORKSHOP_INSTANCE/agent.pid"

if [ "${1:-}" = --restart ] && [ -f "$pid_file" ]; then
  read -r agent_pid < "$pid_file"
  if [[ "$agent_pid" =~ ^[0-9]+$ ]] && [ -r "/proc/$agent_pid/cmdline" ]; then
    mapfile -d '' -t agent_command < "/proc/$agent_pid/cmdline"
    if [ "${agent_command[0]##*/}" != devfest ] || [ "${agent_command[1]:-}" != agent ]; then
      echo 'Refusing to restart an unrelated process.' >&2
      exit 1
    fi
    kill -TERM "$agent_pid"
    for attempt in {1..50}; do
      kill -0 "$agent_pid" 2>/dev/null || break
      sleep 0.1
    done
    if kill -0 "$agent_pid" 2>/dev/null; then
      echo 'Agent has not stopped; check its log before restarting.' >&2
      exit 1
    fi
  fi
fi

echo "$$" > "$pid_file"
exec ./bin/devfest agent
