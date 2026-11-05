# Run the workshop

Use the installed Go **1.27.3**, PostgreSQL **17** and Temporal versions. Keep the existing infrastructure and databases. Each person runs the same application on their own machine; there is no presenter/participant setting.

## Configure and prepare

The repository-root `.env` needs:

```dotenv
DATABASE_URL=postgres://user:password@127.0.0.1:5432/your_database?sslmode=disable
GEMINI_API_KEY=your-gemini-api-key
```

Use your actual provisioned connection string and key. Both Go services load `.env` automatically; exported variables take precedence. The file is ignored by Git and is not executed as shell code. Gemini is always the workshop model. `GEMINI_MODEL` is optional and defaults to `gemini-3.6-flash`; there is no live/scripted switch.

```bash
make setup
```

This builds the binaries, initializes the configured local database if missing, migrates and seeds John/Zainab. Existing balances and beneficiaries are preserved. `./bin/payment database-check` checks connectivity without changing data. No roles, databases or ports are selected based on the person presenting or attending.

## Run in separate terminals

Start Temporal only if it is not already running:

```bash
make temporal
```

In a payment-service terminal:

```bash
make payment
```

In an agent terminal:

```bash
make agent
```

The agent combines REST API and Temporal worker. The payment service runs independently. Stop either command with **Ctrl-C**. After editing Go code, stop the agent and run `make agent` again; Go recompiles it. There is no automatic process remediation or restart target.

Defaults are API `127.0.0.1:8090`, provider `127.0.0.1:8091`, Temporal `127.0.0.1:7233`, UI <http://localhost:8233>, and queue `devfest26-workshop`. An existing occupied port means its running process should be stopped manually before starting a replacement. The services use one fixed simulated account and the configured database.

## Try it

In a CLI terminal:

```bash
./bin/pay ask 'List my beneficiaries'
./bin/pay wait REQUEST_ID
./bin/pay ask 'Send ₦100 to John'
./bin/pay wait REQUEST_ID
./bin/pay approve REQUEST_ID
./bin/pay wait --terminal REQUEST_ID
```

Keep the same request ID across an agent restart. Finish requests before changing tool declarations. The [one-file exercise](beneficiary-exercise.md) adds beneficiary creation; existing reads and payments work beforehand. Model audit events are saved to `.local/model-invocations.jsonl`, without prompts, hidden reasoning or API keys.

## Checks and presentation

```bash
make preflight       # Database, Temporal and one actual Gemini call
make test            # Unit tests and static checks
make integration     # Actual payment Handler/Store/HTTP + PostgreSQL
make check           # Actual Gemini/Temporal/PostgreSQL, using the written solution
make slides
```

`make check` creates isolated schemas and free service ports, owns its test processes, and cleans up only those fixtures. It does not manage an already-running workshop process or overwrite the exercise file. It incurs model calls and requires `psql`. By default prompts are paced for a low quota; use `CHECK_PAUSE_SECONDS=0 make check` with sufficient project quota. Results are in `.local/verification/`.

Deterministic model fixtures are unit-test code only. If internet/model service is unavailable during the talk, show an explicitly labelled recording instead of changing application modes. Run `source scripts/env.sh && python3 scripts/demo.py` with both services running to record a rehearsal; it asks you to stop/restart the agent manually at the recovery step.

For slides, install Node dependencies once with `npm ci`. Run `node scripts/capture-artifacts.mjs` after export to verify all 21 slides and offline fonts. The PDF embeds Google Sans/Roboto Mono; keep `dist/assets/` beside the HTML. Set `CHROME_PATH` if needed on WSL. Existing Marp development dependency audit advisories remain; no forced downgrade was applied.

The [repository rules](../AGENTS.md) apply to source and solutions. Existing Compose/PostgreSQL versions, volumes and data are unchanged; Docker remains optional and unverified in this WSL environment. All payments are simulated.
