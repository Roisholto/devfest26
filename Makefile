SHELL := /bin/bash
export GOTOOLCHAIN := go1.27.3
.PHONY: build setup postgres temporal migrate seed preflight start worker test integration vet slides reset
build:
	go build -o bin/devfest ./cmd/devfest
	go build -o bin/pay ./cmd/pay
postgres:
	bash scripts/postgres.sh
temporal:
	bash scripts/temporal.sh
migrate: build
	source scripts/env.sh && ./bin/devfest migrate
seed: build
	source scripts/env.sh && ./bin/devfest seed
setup: postgres migrate seed
preflight:
	source scripts/env.sh && ./bin/devfest preflight
start:
	bash scripts/start.sh
worker:
	bash scripts/worker.sh
test:
	go test ./...
integration:
	source scripts/env.sh && TEST_DATABASE_URL="$$PAYMENT_DATABASE_URL" go test -race -count=1 ./internal/payment
vet:
	go vet ./...
slides:
	npm run slides
reset:
	bash scripts/reset.sh $(CONFIRM)
exercise-check:
	source scripts/env.sh && python3 scripts/exercise-check.py
demo:
	source scripts/env.sh && python3 scripts/demo.py
lab-check:
	source scripts/env.sh && python3 scripts/lab-check.py $(LAB)
verify:
	source scripts/env.sh && TEST_DATABASE_URL="$$PAYMENT_DATABASE_URL" go test -race ./...
	go vet ./...
