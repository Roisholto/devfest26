# Run the workshop

Use the installed Go **1.27.3**, PostgreSQL **17** and Temporal versions. Infrastructure versions, database contents and Docker configuration stay as configured. Node/npm and Chrome are needed only to export slides.

The runtime has **two Go processes**:

- `devfest agent`: REST API, Google ADK and Temporal worker together.
- `devfest payment`: simulated provider and PostgreSQL persistence.

## Start

Put `DATABASE_URL` and, for live inference, `GEMINI_API_KEY` or `GOOGLE_API_KEY` in the root `.env`. Go binaries and launch scripts load it automatically; exported variables take precedence. The file is ignored by Git and never evaluated as shell code.

```bash
make setup
```

This builds the binaries, initializes the configured local database if missing, and applies migrations and idempotent seeds. Without an explicit URL it uses the existing managed PostgreSQL 17 cluster on port 55432. It never resets an account balance. `./bin/devfest database-check` checks connectivity without changing schema or data.

Start Temporal in its own terminal **if it is not already running**:

```bash
make temporal
```

In the workshop terminal:

```bash
make start
```

Temporal uses port 7233 and its UI is at <http://localhost:8233>. Its existing development persistence is separate from the payment database. The default model is the explicitly labelled scripted adapter, which works offline after dependencies are installed. For Gemini, start with `MODEL_MODE=live make start` or set `MODEL_MODE=live` in `.env`; no automatic model fallback occurs.

## Try the working agent

In another terminal, from the same repository:

```bash
./bin/pay ask 'List my beneficiaries'
./bin/pay wait REQUEST_ID
./bin/pay ask 'Send ₦100 to John'
./bin/pay wait REQUEST_ID
./bin/pay approve REQUEST_ID
./bin/pay wait --terminal REQUEST_ID
```

Flags go before the prompt or ID. John and Zainab are seeded, so payments work before the exercise. Beneficiary creation is intentionally unfinished in `internal/agent/beneficiary.go`; complete the [one-file exercise](beneficiary-exercise.md) to add Ayomide. Stay on `main` throughout.

## Edit and restart

After editing the exercise file:

```bash
make restart-agent
```

This builds the new code and restarts the combined API/worker. The payment provider remains running. During the restart the agent API is temporarily unavailable; after restart use the same request ID. `make agent` starts an agent that has already been stopped. Ctrl-C in the original `make start` terminal stops the provider and any restarted agent it owns.

Logs/PIDs are in `.local/<instance>/agent.log`, `agent.pid`, `payment.log` and `payment.pid`. Model invocation audit lines contain workflow/Activity identifiers, not prompts or API keys.

## Environments and data

The participant defaults are API `127.0.0.1:8090`, provider `127.0.0.1:8091`, queue `devfest26-participant`. A presenter can export `WORKSHOP_INSTANCE=presenter` in each terminal to use ports 8080/8081 and queue `devfest26-presenter`. Use fresh shells when changing instances. With an external `DATABASE_URL`, use separate database URLs if both instances run on one machine; one URL otherwise shares payment data.

Queue names are fixed by instance, independent of Git. `TEMPORAL_TASK_QUEUE` is used only to isolate automated verification runs. This layout uses new queue names; earlier histories remain in Temporal and existing databases remain intact. Finish requests before changing the exercise's advertised tools.

The optional `compose.yaml` retains `postgres:17`, volume `workshop-postgres` and `/var/lib/postgresql/data`. Docker remains optional and was unavailable in this WSL environment. Native scripts use the installed PostgreSQL 17 binaries; set `PG_BIN` if needed.

## Verification and slides

```bash
make test             # Unit tests, including Temporal's test environment
make integration      # Real production payment Handler/Store/HTTP + PostgreSQL
make demo-check       # Real two-process starter, crash and response-loss recovery
make solution-check   # Compile the written exercise solution in a temporary copy
make exercise-check   # Check YOUR completed exercise against running services
```

`make live-check` runs the supplied solution with real Gemini in an isolated schema. It incurs model calls and requires Temporal, PostgreSQL and `psql`. It pauses between prompts for low project quotas; with sufficient quota use `LIVE_CHECK_PAUSE_SECONDS=0 make live-check`. Verification never replaces your exercise file or debits the regular demo account. Results are under `.local/verification/<run>/`.

```bash
npm ci
make slides
node scripts/capture-artifacts.mjs
```

The PDF embeds the supplied template's Google Sans/Roboto Mono families. Keep `dist/assets/` next to `dist/workshop.html` for offline HTML. The capture script verifies actual font rendering offline and checks all 21 slides for overflow. Set `CHROME_PATH` to a native browser if needed on WSL. Marp's development dependency audit advisories remain documented; do not force an incompatible downgrade.

The [repository rules](../AGENTS.md) apply to source and solutions. Mocks are unit coverage; integration and demo checks use actual production types and real infrastructure. All payments are simulated.
