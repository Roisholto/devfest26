SHELL := /bin/bash
.SHELLFLAGS := -eu -o pipefail -c
.ONESHELL:
export GOTOOLCHAIN := go1.27.3

.PHONY: build build-agent build-payment build-cli setup postgres temporal migrate seed preflight start agent payment restart-agent test integration vet slides reset demo-check solution-check live-check exercise-check demo verify

build: build-agent build-payment build-cli

build-agent:
	mkdir -p bin
	go build -o bin/agent ./cmd/agent

build-payment:
	mkdir -p bin
	go build -o bin/payment ./cmd/payment

build-cli:
	mkdir -p bin
	go build -o bin/pay ./cmd/pay

# Keep the nontrivial, existing cluster/role provisioning in one helper.
postgres:
	bash scripts/postgres.sh

temporal:
	mkdir -p .local
	exec temporal server start-dev --db-filename "$$PWD/.local/temporal.db" --ip 127.0.0.1 --ui-ip 127.0.0.1

migrate: build-payment
	./bin/payment migrate

seed: build-payment
	./bin/payment seed

setup: build
	source scripts/env.sh
	task_default_dsn="postgres://devfest26_$${WORKSHOP_INSTANCE}@127.0.0.1:$${PG_WORKSHOP_PORT}/devfest26_$${WORKSHOP_INSTANCE}?sslmode=disable"
	if [ -z "$${DATABASE_URL:-}" ] && [ "$$PAYMENT_DATABASE_URL" = "$$task_default_dsn" ]; then
	  bash scripts/postgres.sh
	fi
	./bin/payment database-init
	./bin/payment migrate
	./bin/payment seed

preflight: build-payment build-agent
	./bin/payment preflight
	./bin/agent preflight

# Both services can also be run independently in separate terminals.
agent: build-agent
	source scripts/env.sh
	mkdir -p ".local/$$WORKSHOP_INSTANCE"
	echo $$$$ > ".local/$$WORKSHOP_INSTANCE/agent.pid"
	exec ./bin/agent

payment: build-payment
	source scripts/env.sh
	mkdir -p ".local/$$WORKSHOP_INSTANCE"
	echo $$$$ > ".local/$$WORKSHOP_INSTANCE/payment.pid"
	exec ./bin/payment

restart-agent: build-agent
	@source scripts/env.sh
	pid_file=".local/$$WORKSHOP_INSTANCE/agent.pid"
	if [ -f "$$pid_file" ]; then
	  read -r agent_pid < "$$pid_file"
	  if [[ "$$agent_pid" =~ ^[0-9]+$$ ]] && [ -r "/proc/$$agent_pid/cmdline" ]; then
	    mapfile -d '' -t agent_command < "/proc/$$agent_pid/cmdline"
	    case "$${agent_command[0]:-}" in
	      ./bin/agent|"$$PWD/bin/agent") ;;
	      *) echo 'Refusing to restart an unrelated process.' >&2; exit 1 ;;
	    esac
	    kill -TERM "$$agent_pid"
	    for attempt in {1..50}; do
	      kill -0 "$$agent_pid" 2>/dev/null || break
	      sleep 0.1
	    done
	    if kill -0 "$$agent_pid" 2>/dev/null; then
	      echo 'Agent has not stopped; inspect its log before restarting.' >&2
	      exit 1
	    fi
	  fi
	fi
	mkdir -p ".local/$$WORKSHOP_INSTANCE"
	echo $$$$ > "$$pid_file"
	exec ./bin/agent

# The agent may be crashed/restarted independently; the provider stays alive.
start: build
	@source scripts/env.sh
	./bin/payment preflight
	./bin/agent preflight
	mkdir -p ".local/$$WORKSHOP_INSTANCE"
	for address in "$$API_ADDRESS" "$$PAYMENT_ADDRESS"; do
	  python3 -c 'import socket,sys; host,port=sys.argv[1].rsplit(":",1); host=host.strip("[]"); probe=socket.socket(socket.AF_INET6 if ":" in host else socket.AF_INET); probe.setsockopt(socket.SOL_SOCKET,socket.SO_REUSEADDR,1); probe.bind((host,int(port))); probe.close()' "$$address"
	done
	pids=()
	cleanup() {
	  for pid in "$${pids[@]}"; do kill "$$pid" 2>/dev/null || true; done
	  local pid_file=".local/$$WORKSHOP_INSTANCE/agent.pid"
	  if [ -f "$$pid_file" ]; then
	    local agent_pid
	    read -r agent_pid < "$$pid_file"
	    if [[ "$$agent_pid" =~ ^[0-9]+$$ ]] && [ -r "/proc/$$agent_pid/cmdline" ]; then
	      local -a agent_command
	      mapfile -d '' -t agent_command < "/proc/$$agent_pid/cmdline"
	      case "$${agent_command[0]:-}" in
	        ./bin/agent|"$$PWD/bin/agent") kill -TERM "$$agent_pid" 2>/dev/null || true ;;
	      esac
	    fi
	  fi
	}
	trap cleanup EXIT INT TERM
	for service in payment agent; do
	  "./bin/$$service" > ".local/$$WORKSHOP_INSTANCE/$$service.log" 2>&1 &
	  pids+=("$$!")
	  echo "$$!" > ".local/$$WORKSHOP_INSTANCE/$$service.pid"
	done
	sleep 0.3
	for pid in "$${pids[@]}"; do
	  if ! kill -0 "$$pid" 2>/dev/null; then echo "Service exited; inspect .local/$$WORKSHOP_INSTANCE/*.log" >&2; exit 1; fi
	done
	echo "Services started: instance=$$WORKSHOP_INSTANCE mode=$$MODEL_MODE API=http://$$API_ADDRESS"
	echo 'Restart the agent (API + worker): make restart-agent'
	wait "$${pids[0]}"

test:
	go test ./...

integration:
	source scripts/env.sh
	TEST_DATABASE_URL="$$PAYMENT_DATABASE_URL" go test -race -count=1 ./internal/payment

vet:
	go vet ./...

slides:
	npm run slides

# The helper guards against resetting an external database.
reset: build-payment
	bash scripts/reset.sh $(CONFIRM)

exercise-check:
	source scripts/env.sh
	python3 scripts/exercise-check.py

demo:
	source scripts/env.sh
	python3 scripts/demo.py

demo-check: build
	source scripts/env.sh
	python3 scripts/check-demo.py

solution-check: build
	source scripts/env.sh
	python3 scripts/check-demo.py --solution

live-check: build
	source scripts/env.sh
	python3 scripts/check-demo.py --live --solution

verify:
	source scripts/env.sh
	TEST_DATABASE_URL="$$PAYMENT_DATABASE_URL" go test -race ./...
	go vet ./...
