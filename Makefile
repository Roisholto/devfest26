SHELL := /bin/bash
export GOTOOLCHAIN := go1.27.3

.PHONY: build setup agent payment temporal preflight test integration check slides

build:
	mkdir -p bin
	go build -o bin/agent ./cmd/agent
	go build -o bin/payment ./cmd/payment
	go build -o bin/pay ./cmd/pay

setup: build
	./bin/payment database-init
	./bin/payment migrate
	./bin/payment seed

# Run these in separate terminals. Stop with Ctrl-C before running again.
agent:
	go run ./cmd/agent

payment:
	go run ./cmd/payment

temporal:
	mkdir -p .local
	temporal server start-dev --db-filename "$(CURDIR)/.local/temporal.db" --ip 127.0.0.1 --ui-ip 127.0.0.1

preflight:
	go run ./cmd/payment preflight
	go run ./cmd/agent preflight

test:
	go test -race ./...
	go vet ./...

integration:
	source scripts/env.sh && TEST_DATABASE_URL="$$PAYMENT_DATABASE_URL" go test -race -count=1 ./internal/payment

# Uses real Gemini, Temporal and PostgreSQL; verifies the supplied solution.
check: build
	source scripts/env.sh && python3 scripts/check-demo.py --solution

slides:
	npm run slides
