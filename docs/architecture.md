# Two Go processes, two workflows

Gemini requests tools; Go validates and controls them; Temporal preserves progress. The payment service enforces the financial effect. This remains the README's core objective.

## Runtime

`cmd/devfest` starts one of two long-running commands:

- **agent**: REST API and Temporal worker in one process, sharing one Temporal client. `internal/api` owns requests/approval; `internal/agent/worker.go` owns registrations.
- **payment**: the simulated provider, its HTTP boundary and PostgreSQL storage. Only this service accesses payment tables.

Temporal and PostgreSQL retain their existing versions and persistence. Restarting/crashing the agent temporarily removes both its API and its worker; the provider can continue processing independently. The launcher keeps the provider alive during agent restarts.

## Read four files

1. `internal/agent/beneficiary.go`: the only participant edit; declaration plus handler.
2. `internal/agent/tools.go`: supplied catalog, strict decoding, allowlist and native Dynamic Activity dispatcher.
3. `internal/agent/agent_workflow.go`: ADK reasoning, proposal and explicit approval.
4. `internal/agent/payment_workflow.go`: approved execution and bounded reconciliation.

There are no checkpoint constants or notification/scheduling feature flags. `main` is a working starter with one incomplete tool. `docs/solutions/beneficiary.go.txt` is its complete replacement; verification compiles that actual file in a temporary source copy. Follow-up guides describe extensions to the same checkout.

## Durable calls

The agent workflow starts before reasoning. ADK model calls run as statically registered Activities. Every model tool runs through the one native `RegisterDynamicActivity` handler. Calling a static Activity by string name is different from native dynamic fallback resolution; model tool names are deliberately not individually registered.

Gemini can request several tools in a turn. Each adapter call recovers its own coroutine context through `googleadk.WorkflowContext(toolCtx)` before waiting. A preparation-in-flight guard preserves one proposal even for concurrent duplicate requests. The ordered catalog remains stable within a running workflow; finish requests before editing declarations.

Model Activities use explicit ADK options: at most three attempts, a 40-second attempt timeout, a 100-second total budget and 15–30-second backoff. The agent permits eight turns. The Gemini SDK makes one attempt so it cannot multiply Temporal retries. Completed recorded calls are reused in replay; interrupted/unrecorded calls can still incur repeat charges. Audit events record identifiers rather than credentials, prompts or hidden reasoning.

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

Fixed queues are `devfest26-participant` and `devfest26-presenter`; they do not depend on Git. Verification overrides `TEMPORAL_TASK_QUEUE` and uses isolated PostgreSQL schemas and free loopback ports. Earlier histories remain stored under their original queues. No existing database, infrastructure version or history is reset by the reorganization.

Services bind to localhost and use one fixed simulated account. Authentication infrastructure is outside this workshop.

## Sources

- [Temporal workflows and replay](https://docs.temporal.io/workflows)
- [Dynamic Activities in Go](https://docs.temporal.io/develop/go/activities/dynamic-activity)
- [Temporal Google ADK integration](https://github.com/temporalio/ai-integrations/tree/main/go/googleadk)
- [PostgreSQL locking](https://www.postgresql.org/docs/17/explicit-locking.html)

The supplied first-video transcript page returned HTTP 403; it is linked rather than claimed as reviewed. The supplied second-video text is a description.
