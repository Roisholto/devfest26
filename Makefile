SHELL := /bin/bash
export GOTOOLCHAIN := go1.27.3

.PHONY: setup api worker payment temporal preflight test integration check slides

setup:
	go run ./cmd/payment database-init
	go run ./cmd/payment migrate
	go run ./cmd/payment seed

# Run each service in its own terminal. Stop with Ctrl-C before running again.
api:
	go run ./cmd/api

worker:
	go run ./cmd/worker

payment:
	go run ./cmd/payment

temporal:
	mkdir -p .local
	temporal server start-dev --db-filename "$(CURDIR)/.local/temporal.db" --ip 127.0.0.1 --ui-ip 127.0.0.1

preflight:
	go run ./cmd/payment preflight
	go run ./cmd/worker preflight

test:
	go test -race ./...
	go vet ./...

integration:
	source scripts/env.sh && TEST_DATABASE_URL="$$PAYMENT_DATABASE_URL" go test -race -count=1 ./internal/payment

# Uses real Gemini, Temporal and PostgreSQL; verifies the supplied solution.
check:
	source scripts/env.sh && python3 scripts/check-demo.py --solution

slides:
	npm run slides
