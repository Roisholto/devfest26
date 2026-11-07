# Three Go processes, two workflows

Gemini requests tools; Go validates and controls them; Temporal preserves progress. The payment service enforces the financial effect. This remains the README's core objective.

## Runtime

Each service has its own entry point, run directly with Go:

- **`cmd/api` / `make api`**: accepts requests and forwards Workflow starts, queries and approval Updates to Temporal. It executes no workflows or model calls and needs no model credential.
- **`cmd/worker` / `make worker`**: polls Temporal, runs ADK workflows, model Activities, Dynamic tool Activities and internal payment Activities. It alone needs `GEMINI_API_KEY`. Preflight and history replay belong here.
- **`cmd/payment` / `make payment`**: simulated provider, HTTP validation and PostgreSQL storage. Only this service accesses payment tables. Database initialization/migration/seed/checks belong here.

Run each in a separate terminal; stop/restart only the worker for recovery. Commands use `go run`; there is no binary build or process supervisor. Temporal and PostgreSQL retain their existing versions and persistence.

When the worker is absent, the API can check its own Temporal connection and submit new requests to the task queue. Health does not assert that a worker is polling. Live Workflow queries and decision Updates require a worker; they can time out while it is stopped. Temporal retains execution history and pending work, and approval deadlines continue to advance. After restarting before expiry, retrieve the same proposal, then approve.

## Without and with durable execution

An ordinary agent loop held only in process memory loses its conversation, tool results, proposal and approval wait when that process exits. Restarting the program cannot reconstruct them on its own; the application must implement persistence/recovery or start the request again, potentially repeating model calls and tools.

Here, `AgentWorkflow` starts **before the first model call**. Model and tool Activity completions are recorded in Temporal. On worker recovery, deterministic Workflow code replays those results to rebuild the ADK session, proposal and approval wait. It continues unfinished work for the same Workflow ID instead of generating a new proposal. This is an execution history, not a snapshot of process memory.

Completed, recorded calls do not run again during replay. Calls whose completion was not recorded can be retried and charged again. Both ordinary and durable agents need idempotent external mutations. PostgreSQL uniqueness/transactions and the original payment key protect the debit even if an Activity attempt repeats. Durability guarantees recovery of progress, not guaranteed successful payment or automatic compensation.

The slides compare these behaviors; this repository intentionally implements one durable execution path, without another runtime mode.

## Read four files

1. `internal/agent/beneficiary.go`: the participant implements the typed `AddBeneficiary` handler.
2. `internal/agent/tools.go`: the ordered `Catalog()`, a small typed dispatch switch and named `ToolDispatcher` methods for listing, search, lookup, balance, status and proposal preparation. The participant adds one catalog entry here.
3. `internal/agent/agent_workflow.go`: `WorkflowInput`, queryable `WorkflowState`, ADK reasoning and explicit `ApprovalDecision` handling.
4. `internal/agent/payment_workflow.go`: execution of an approved `domain.TransferProposal`, provider submission and bounded reconciliation.

`PrepareTransferArgs.AmountNaira` holds the model's exact decimal string. The preparation Activity validates it and builds a `domain.TransferProposal` with integer `AmountKobo`; `domain.SubmitTransferRequest` carries those canonical values to the provider. Each type's GoDoc explains its role and boundary. Inspect it in the editor or from the terminal:

```bash
go doc ./internal/agent PrepareTransferArgs
go doc ./internal/agent WorkflowState
go doc ./internal/domain TransferProposal
```

There are no checkpoint constants or notification/scheduling feature flags. `main` is a working starter with one incomplete tool. `docs/solutions/catalog.go.txt` supplies the complete catalog function, and `docs/solutions/beneficiary.go.txt` supplies the handler file. Verification runs those exact references in a temporary source copy. Follow-up guides describe extensions to the same checkout.

## Durable calls

The agent workflow starts before reasoning. ADK model calls run as statically registered Activities. Every model tool runs through the one native `RegisterDynamicActivity` handler. Calling a static Activity by string name is different from native dynamic fallback resolution; model tool names are deliberately not individually registered.

Gemini can request several tools in a turn. Each adapter call recovers its own coroutine context through `googleadk.WorkflowContext(toolCtx)` before waiting. A preparation-in-flight guard preserves one proposal even for concurrent duplicate requests. The ordered catalog remains stable within a running workflow; finish requests before editing declarations.

Model Activities use explicit ADK options: at most three attempts, a 40-second attempt timeout, a 100-second total budget and 15–30-second backoff. The agent permits eight turns. The Gemini SDK makes one attempt so it cannot multiply Temporal retries. Completed recorded calls are reused in replay; interrupted/unrecorded calls can still incur repeat charges. Audit events record identifiers rather than credentials, prompts or hidden reasoning.

Beneficiary listing and name search return up to 50 matches for the demo. Once a recipient ID is known, preparation calls `GET /beneficiaries/{id}` for a direct, demo-account-scoped lookup. It then checks replacement eligibility and balance before producing a proposal. A valid recipient can be used even when it is outside the listing window.

## Validation and financial effect

Every tool is validated. Adding a beneficiary from a complete user instruction needs no extra prompt. Sending money or replacing a definitively failed transfer requires a new canonical proposal and explicit human approval.

- Account numbers are strings of exactly ten ASCII digits, preserving leading zeros.
- Amounts use exact decimal-string conversion into integer kobo.
- The dispatcher, service and PostgreSQL independently enforce their boundaries.
- Stable beneficiary operation keys and bank/account uniqueness protect repeated additions.
- Stable payment keys bind recipient, amount, currency and scenario.
- A row-locked PostgreSQL transaction checks funds, debits and writes the journal atomically.
- After a lost response, the child workflow reads the original key before considering another submission.
- After two minutes of uncertainty, return `needs_attention`; explicit reconciliation only reads status.

The response-loss scenario closes a real TCP connection after the provider commits. A timeout does not prove failure. PostgreSQL durability and Temporal persistence serve different responsibilities; this is a simulated debit journal, not a full banking ledger.

## Interfaces and isolation

The API exposes request creation/state, explicit proposal decisions, transaction lookup and status-only reconciliation. JSON is strict and bounded. Request keys reject changed payloads; decision Update acknowledgements bind the actual decision even if an Update ID is reused. Actor state and references come from the workflow/provider, not model narration.

The CLI completes the request/response interaction within one invocation. `ask` starts a Workflow then uses the existing state endpoint internally until it has an answer or approval proposal. `approve`/`reject` submit the explicit decision then wait for the outcome. A context deadline covers the whole command, including in-flight HTTP requests and polling delays. Deadline responses preserve the request ID, keys and last observed state, with a command to resume. `wait` uses the same logic. Request-context cancellation stops local waiting; the CLI does not cancel Temporal Workflows. Unknown submission/decision acknowledgements remain errors with keys retained for safe retry.

The default queue is `devfest26-workshop`, independent of the person running it and of Git. Verification overrides `TEMPORAL_TASK_QUEUE` and uses isolated PostgreSQL schemas and free loopback ports. Earlier histories remain stored under their original queues. No existing database, infrastructure version or history is reset by the reorganization.

There are no presenter/participant or live/scripted settings. Production execution always uses Gemini; the deterministic model fixture is compiled only in unit tests. Each computer uses its configured database. Services bind to localhost and use one fixed simulated account. Authentication infrastructure is outside this workshop.

## Sources

- [Temporal workflows and replay](https://docs.temporal.io/workflows)
- [Dynamic Activities in Go](https://docs.temporal.io/develop/go/activities/dynamic-activity)
- [Temporal Google ADK integration](https://github.com/temporalio/ai-integrations/tree/main/go/googleadk)
- [PostgreSQL locking](https://www.postgresql.org/docs/17/explicit-locking.html)
