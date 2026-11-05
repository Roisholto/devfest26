# Responsibilities and boundaries

Gemini proposes tool calls; Go validates and controls their execution. Every tool is validated; only sending money requires additional confirmation. Temporal preserves progress. PostgreSQL transactions and stable payment keys enforce the financial effect.

The **agent workflow** starts with the request and runs ADK through `googleadk.NewContext`. Gemini calls are Activities. Each model tool schedules an Activity under its own tool name. A native `RegisterDynamicActivity` handler selects only an allowlisted tool, decodes its Go input and performs validation. The **payment workflow** starts as a child only after the application has accepted the exact proposal's approval Update.

Static Activities may also be called by string name. Native dynamic resolution is different: unregistered Activity types reach the one fallback dispatcher. Individual tool names are deliberately not statically registered. Model invocation and internal `SubmitTransfer`/`ReadTransfer` Activities are separately registered and absent from the model's tool catalog.

All I/O stays out of workflow code. Activities access the simulated payment service over HTTP; only the service accesses PostgreSQL. Workflow code uses Temporal time/timers and captures tool results in deterministic state. Tool schemas guide the model; runtime validation is authoritative. A failed tool prevents subsequent tool scheduling in that request and the user can submit a corrected instruction.

Gemini can return multiple tool calls in a single turn. The adapter obtains each calling coroutine's context through `googleadk.WorkflowContext(toolCtx)` before waiting on an Activity; a context captured from the root workflow cannot safely block a parallel tool coroutine. A preparation-in-flight guard preserves the one-proposal boundary when the model requests duplicate preparations concurrently. Calls already scheduled before another tool fails can still complete; the failure prevents subsequent tool scheduling.

Model retry limits are passed explicitly to `googleadk.NewModel` using `WithModelActivityOptions`; the bridge replaces inherited workflow Activity options. Each model Activity has at most three attempts, a 40-second attempt timeout and a 100-second total budget, with 15–30-second backoff. The agent permits at most eight model turns. The underlying Gemini SDK makes one attempt so it cannot multiply Temporal's retries. Completed recorded calls are reused during replay; an interrupted or unrecorded call may still incur repeated model charges.

## Payment correctness

- `BIGINT` kobo, exact string-to-kobo conversion; no floating-point financial writes.
- Ten ASCII digits in a string for account numbers; leading zeros preserved.
- Beneficiary operation keys plus bank/account uniqueness protect retried additions.
- Transfer keys bind immutable recipient, amount, currency and simulation details.
- Transfer-row lock, account-row lock, balance check, debit and journal entry occur in one PostgreSQL transaction.
- A connection/commit error means the outcome may be unknown. Check the original key.
- The response-loss scenario closes the TCP connection **after** committing the debit.
- A two-minute reconciliation budget returns `needs_attention`; the explicit reconcile endpoint only reads the original payment. It never creates a replacement.
- Failed-transfer retries must match a definitively failed original; they produce a new proposal and require fresh approval.

This is a simplified simulated debit journal, not a full accounting ledger. PostgreSQL durability and Temporal persistence are separate. Durable execution does not mean automatic business compensation, zero repeated model charges, or guaranteed successful payments.

## REST interfaces

The app exposes POST `/requests`, GET `/requests/{id}`, POST `/requests/{id}/decision`, GET `/transactions/{reference}`, and POST `/transactions/{reference}/reconcile`. Bodies use strict bounded JSON. `/requests` takes `prompt`, optional `scenario` and optional `approval_seconds`, plus `Idempotency-Key`. It returns request/workflow IDs and explicit model mode. Decisions require `proposal_id`, an explicit boolean `approve`, and a stable Update idempotency key. GET returns canonical proposal/state/result, not merely model prose.

The payment service exposes beneficiary list/search/create, balance, transfer submit/status/last, inspection evidence and a simulated receipt sink. It independently checks all invariants. Both services use a fixed local account rather than authentication infrastructure.

The demo does not promise production security or measured scalability. Services listen only on loopback. Hidden model reasoning and API credentials are not logged; invocation audit logs retain workflow/run/Activity/attempt identifiers for observable recovery evidence.

## Sources

- [Temporal workflows and replay](https://docs.temporal.io/workflows)
- [Dynamic Activities in Go](https://docs.temporal.io/develop/go/activities/dynamic-activity)
- [Temporal Google ADK integration](https://github.com/temporalio/ai-integrations/tree/main/go/googleadk)
- [PostgreSQL locking](https://www.postgresql.org/docs/17/explicit-locking.html)
- [OpenAI APIs/Agents SDK comparison reference](https://developers.openai.com/api/docs/guides/agents)

The supplied first video transcript page returned HTTP 403; it is linked, not claimed as reviewed. The supplied second-video text is a description. Official documentation supports the technical explanations.
