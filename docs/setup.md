# Setup before the workshop

Use Linux/WSL, Bash, Go **1.27.3**, PostgreSQL **17** binaries, Temporal CLI, Python 3, and Node/npm for the slides. The application uses Go `net/http`, `http.ServeMux`, `pgx/v5`/`pgxpool`, parameterized SQL, embedded versioned SQL migrations, and `log/slog`. There is no ORM or application authentication layer. Services bind to localhost and use one fixed simulated NGN account.

The learner contract: **extend a working Go payment agent**, implement one tool, and explore recovery. Listing/search and payment-service persistence are supplied. All accounts and money are simulated.

## 1. Select the environment and download dependencies

From the repository root:

```bash
export WORKSHOP_INSTANCE=participant  # presenter for the opening/recovery demos
source scripts/env.sh
go version                         # expected: go1.27.3
go mod download
make build
```

Go's automatic toolchain selection downloads 1.27.3 if needed. This machine already has it cached. Do this before the venue. Set `PG_BIN` if PostgreSQL 17 binaries are somewhere other than `/usr/lib/postgresql/17/bin`.

## 2. Prepare PostgreSQL

```bash
make setup
```

This starts a **separate, user-owned local cluster on port 55432**, creates only `devfest26_presenter` and `devfest26_participant`, and migrates/seeds the selected database. It does not change the system PostgreSQL instance on port 5432 and requires no sudo. Its local connections use trust authentication on loopback for the isolated demo; the application itself has no login/token layer.

Each database is owned by its own role. Both start with ₦100,000 and beneficiaries John and Zainab. The presenter seed also includes Ayomide, GTBank (`058`), account `0123454821`. Participants add Ayomide themselves. Reseeding does not reset balances or duplicate beneficiaries.

For an existing PostgreSQL installation, put `DATABASE_URL` in the repository-root `.env` using your provisioned role/database. Both Go binaries and the shell launcher load it automatically. Exported variables override file values; `DATABASE_URL` is the canonical database setting and `PAYMENT_DATABASE_URL` is a legacy fallback. A URL may be quoted, and `&` is treated as data rather than shell syntax. `.env` is ignored by Git and is not evaluated as shell code.

Run `./bin/devfest database-check` for a connection-only check with no schema/data changes. When ready to initialize the workshop, run `make setup`: it skips the native cluster for an explicitly configured database, creates the selected database if it is missing on a local server and the supplied role has permission, then migrates/seeds it. Hosted databases must already be provisioned. `make migrate seed` only installs tables in an existing database. The reset script refuses external connection strings. Use separate database settings in fresh shells if running presenter and participant instances against externally provisioned databases; a single configured URL otherwise targets the same database for both.

Optional Docker alternative: stop the managed native cluster first, then run `docker compose up -d --wait postgres` and `make migrate seed`. Compose exposes only loopback port 55432 and initializes the same isolated roles/databases. Temporal and the Go processes still run natively. This alternative is supplied but was not run here because Docker is unavailable in this WSL environment.

## 3. Start Temporal

In a separate terminal:

```bash
make temporal
```

Temporal listens on `127.0.0.1:7233`; its UI is at <http://localhost:8233>. Its persistent development store is `.local/temporal.db`, separate from the payment databases. Stop it with Ctrl-C; restart using the same command to preserve histories. This is a development server, not the workshop's PostgreSQL payment service.

## 4. Choose a model explicitly

Default (works without internet after dependencies are installed):

```bash
export MODEL_MODE=scripted
```

Live Gemini:

```bash
export MODEL_MODE=live
export GEMINI_MODEL=gemini-3.6-flash
# GEMINI_API_KEY (or GOOGLE_API_KEY) is loaded from the root .env.
# Alternatively, export it in this shell; exported settings take precedence.
```

The key is read worker-side and never sent in workflow inputs. No automatic model fallback occurs. Scripted mode is labelled in the API and answers; it is a constrained offline model adapter behind the same ADK/Temporal Activity boundary, not general AI reasoning. Supported examples:

- `List my beneficiaries`
- `Search beneficiaries for Ayo`
- `Add Ayomide at GTBank, account 0123454821`
- `What is my balance?`
- `Send ₦10,000 to Ayomide`
- `Check my last transfer and retry it if it failed`

Unsupported offline prompts receive a help response. Missing/ambiguous information requires a corrected new request; this is not a general multi-turn chat application.

Run `make live-check` to verify your actual Gemini key, function calls, validation, approval, worker restart and lost-response reconciliation. Start Temporal first. This command builds the Go binaries, creates a temporary schema in the configured PostgreSQL database, launches isolated services on available loopback ports, and removes the schema/processes when finished. It does not reset or debit the regular demo account. It uses real model calls and requires the `psql` client and permission to create a schema. Evidence is saved under `.local/live-check/<run>/result.json`.

One agent turn can make several model requests. The live check pauses 65 seconds between prompts to accommodate a five-requests-per-minute project quota; allow roughly ten minutes. With sufficient quota, explicitly run `LIVE_CHECK_PAUSE_SECONDS=0 make live-check`. Model Activities are explicitly configured through ADK's `WithModelActivityOptions` for at most three attempts, a 40-second attempt timeout and a 100-second overall budget, with 15–30-second retry backoff. Exhausted provider retries return an error without authorizing a payment. Free-tier quotas and upstream model availability can interrupt a live rehearsal; retain the scripted fallback for the venue.

## 5. Preflight and launch

```bash
make preflight                     # includes a small real model call in live mode
make start                         # leave running
```

In another terminal, source the **same** environment before using `bin/pay`:

```bash
export WORKSHOP_INSTANCE=participant
source scripts/env.sh
./bin/pay ask --key first-list 'List my beneficiaries'
./bin/pay wait REQUEST_ID_FROM_OUTPUT
```

`make start` launches payment service, REST app, and worker. Logs and PID files are under `.local/<instance>/`. Ctrl-C stops managed processes. The worker can be independently stopped during the restart demonstration; API and payment service remain running.

| Setting | Presenter | Participant |
|---|---|---|
| REST app | 127.0.0.1:8080 | 127.0.0.1:8090 |
| Payment service | 127.0.0.1:8081 | 127.0.0.1:8091 |
| Database/role | devfest26_presenter | devfest26_participant |
| Queue and workflow prefix | presenter + checkpoint | participant + checkpoint |

Different computers each run their own local environment. On one computer the two instances may run simultaneously. Use fresh shells when changing instance; previously exported address/DSN overrides remain explicit overrides. Failure scenarios are attached to individual transfers. Resets affect only the selected managed database.

## Payments and CLI flags

```bash
./bin/pay ask --key payment-1 'Send ₦10,000 to Ayomide'
./bin/pay wait REQUEST_ID
./bin/pay approve REQUEST_ID        # or: reject
./bin/pay wait --terminal REQUEST_ID
./bin/pay transaction TX_REFERENCE
./bin/pay reconcile TX_REFERENCE   # status-only; cannot submit a replacement
./bin/pay evidence
```

Put flags **before** the prompt/ID. Generated request keys are printed; retain them for HTTP retries. Reuse the same key only with the same prompt/scenario/expiry. Request deduplication is scoped to Temporal retention, while payment keys remain in PostgreSQL. Repeated approvals use a stable Update ID. Never treat a timeout as a reason to create a new request/payment key.

Scenarios: `--scenario success|decline|unavailable|delayed|lost-response`. Approval defaults to 600 seconds; `--approval-seconds 5` demonstrates expiry. The configurable transfer limit is `TRANSFER_LIMIT_KOBO=10000000` (₦100,000). Restart workers/services after changing configuration; do not change limits midway through a demonstration.

## Checks and slides

```bash
make test
make integration                  # temporary schemas in the configured demo DB
make vet
source scripts/env.sh
GOTOOLCHAIN=go1.27.3 go test -race ./...
npm ci
npm run slides                    # dist/workshop.html and dist/workshop.pdf
node scripts/capture-artifacts.mjs # offline font checks, layout checks, screenshots
```

PDF export needs Chrome/Chromium. Set `CHROME_PATH` to a native browser binary if auto-discovery selects a nonworking WSL snap. No Node tools are needed to run the Go demo. Marp's transitive browser/download tooling currently reports npm audit advisories; use the pinned lockfile with this trusted local deck and an already installed browser. Do not force an incompatible Marp downgrade suggested by npm audit.

Google Sans and Roboto Mono are bundled with their licenses and embedded in the PDF, matching the supplied DevFest template. Keep `dist/assets/` alongside `dist/workshop.html` when sharing the offline HTML. The capture check blocks network requests and verifies the fonts Chromium actually renders, saving `dist/font-check.json`; it also checks all 21 slides for overflow.

Explicit reset, after stopping services and finishing active workflows:

```bash
make reset CONFIRM=--confirm
```

This preserves Temporal histories and audit logs; use fresh request keys afterwards. To stop the managed PostgreSQL cluster: `/usr/lib/postgresql/17/bin/pg_ctl -D .local/postgres stop`.
