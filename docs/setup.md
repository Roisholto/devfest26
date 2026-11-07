# Run the workshop

Use the installed Go **1.27.3**, PostgreSQL **17** and Temporal versions. Keep the existing infrastructure and databases. Each person runs the same application on their own machine; there is no presenter/participant setting.

## Configure and prepare

The repository-root `.env` needs:

```dotenv
DATABASE_URL=postgres://user:password@127.0.0.1:5432/your_database?sslmode=disable
GEMINI_API_KEY=your-gemini-api-key
```

Use your actual provisioned connection string and key. All Go entry points load `.env` automatically; exported variables take precedence. The file is ignored by Git and is not executed as shell code. Gemini is always the workshop model. `GEMINI_MODEL` is optional and defaults to `gemini-3.6-flash`; there is no live/scripted switch.

```bash
make setup
```

This runs Go commands directly, initializes the configured local database if missing, migrates and seeds John/Zainab. Existing balances and beneficiaries are preserved. `go run ./cmd/payment database-check` checks connectivity without changing data. No roles, databases or ports are selected based on the person presenting or attending.

## Run in separate terminals

Start Temporal only if it is not already running:

```bash
make temporal
```

In a payment-service terminal:

```bash
make payment
```

In an API terminal:

```bash
make api
```

In a worker terminal:

```bash
make worker
```

The API, worker and payment service run independently. Stop a command with **Ctrl-C**. After editing tool/workflow code, stop only the worker and run `make worker` again; Go recompiles it. No explicit binary build is needed. There is no automatic process remediation or restart target.

Defaults are API `127.0.0.1:8090`, provider `127.0.0.1:8091`, Temporal `127.0.0.1:7233`, UI <http://localhost:8233>, and queue `devfest26-workshop`. An existing occupied port means its running process should be stopped manually before starting a replacement. The services use one fixed simulated account and the configured database.

## Try it

In a CLI terminal:

```bash
go run ./cmd/pay ask 'List my beneficiaries'
go run ./cmd/pay ask 'Send ₦100 to John'
go run ./cmd/pay approve REQUEST_ID
```

`ask` waits for an answer or a proposal in the same command. Read-only tools and beneficiary creation return their completed results. A payment returns `awaiting_approval` with its canonical proposal and request ID; `approve` waits for the payment result, and `reject` waits for rejection. Approval remains a separate explicit user action.

`--timeout` bounds the entire command, including HTTP calls and waiting; it defaults to 150 seconds. If a request was accepted but is still running at the deadline, the command returns JSON with `wait_timed_out: true`, the request ID, the last observed state when available, and a `resume_command`. This is a pending response with exit status zero. The Workflow keeps running.

```bash
go run ./cmd/pay ask --timeout 2s 'List my beneficiaries'
# If wait_timed_out is true, use the returned request_id:
go run ./cmd/pay wait REQUEST_ID
# Resume an approved payment through its terminal outcome:
go run ./cmd/pay wait --terminal REQUEST_ID
```

`wait` uses the same bounded response behavior. `get` reads the current state once. Ctrl-C stops the CLI wait without canceling the Workflow. Other command errors return a nonzero status. If submission acknowledgement is lost, the CLI returns its request key: retry the same prompt and flags with that key, rather than creating another request. Decision responses identify their key and whether submission was confirmed; an unconfirmed decision should be inspected before retrying with the same key.

Keep the same request ID across a worker restart. Finish requests before changing tool declarations. The [beneficiary exercise](beneficiary-exercise.md) adds beneficiary creation; existing reads and payments work beforehand. While the worker is stopped, API health checks and new request submissions still work, and requests queue in Temporal. Live state queries and decision Updates need a worker and can time out; retrieve/approve after restarting it. Approval deadlines keep advancing while the worker is absent.

Both HTTP APIs log request method, path, response status, duration and response byte count to their service terminals. Headers, query values and request/response bodies are excluded. A deliberately dropped provider response is logged with `status=0` and `hijacked=true`.

Model audit events are saved to `.local/model-invocations.jsonl`, without prompts, hidden reasoning or API keys.

## Checks and slide export

```bash
make preflight       # Database, Temporal and one actual Gemini call
make test            # Unit tests and static checks
make integration     # Actual payment Handler/Store/HTTP + PostgreSQL
make check           # Actual Gemini/Temporal/PostgreSQL, using the written solution
make slides
```

`make check` creates isolated schemas and free service ports, owns its test processes, and cleans up only those fixtures. It does not manage an already-running workshop process or overwrite the exercise file. It incurs model calls and requires `psql`. By default prompts are paced for a low quota; use `CHECK_PAUSE_SECONDS=0 make check` with sufficient project quota. Results are in `.local/verification/`.


For slides, install Node dependencies once with `npm ci`. Run `node scripts/capture-artifacts.mjs` after export to verify all slides and offline fonts. The PDF embeds Google Sans/Roboto Mono; keep `dist/assets/` beside the HTML. Set `CHROME_PATH` if needed on WSL. Existing Marp development dependency audit advisories remain; no forced downgrade was applied.

The [repository rules](../AGENTS.md) apply to source and solutions. Existing Compose/PostgreSQL versions, volumes and data are unchanged; Docker remains optional and unverified in this WSL environment. All payments are simulated.
