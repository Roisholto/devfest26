## Building a Durable AI Payment Agent with Gemini and Temporal

**Session Type:** Workshop — 1 hour

### Session Overview

AI agents become significantly more interesting when they move beyond answering questions and start performing real-world operations.

In this hands-on workshop, participants will build an AI-powered payment agent that accepts natural-language instructions such as:

“Send ₦5,000 to John”

or

“Check the status of my last transfer and retry it if it failed.”

Google Gemini will be used as the reasoning and tool-selection layer, while Temporal will provide durable execution for payment operations that may involve network failures, delayed provider responses, retries, approvals, and long-running processes.

Rather than allowing the AI model to directly perform financial operations, the workshop will demonstrate how to place deterministic controls around the agent. Gemini decides which approved tool should be invoked, while the application validates the request and Temporal manages the execution of the payment workflow.

The goal is to demonstrate how AI agents can safely interact with critical backend systems without sacrificing reliability, traceability, or control.

### What We Will Build

Participants will build a simplified payment agent capable of:

1. Understanding natural-language payment instructions.
2. Using Gemini function calling to select appropriate payment tools.
3. Looking up beneficiaries.
4. Checking account balances.
5. Initiating a simulated transfer.
6. Checking the status of an existing transaction.
7. Handling transient payment-provider failures.
8. Preventing duplicate transfers through idempotency.
9. Requiring confirmation before executing sensitive payment operations.
10. Persisting payment workflow state with Temporal.
11. Recovering from application or provider failures.
12. Returning the final transaction result to the user.

### Example Flow

**User**

“Send ₦10,000 to Ayomide.”

**Agent**

Determines that the `find_beneficiary` tool is required.

The application retrieves the beneficiary and supplies the result back to Gemini.

Gemini requests:

`check_balance`

The application verifies that sufficient funds are available.

The agent prepares the transaction and asks:

“Send ₦10,000 to Ayomide — GTBank ending in 4821?”

**User**

“Yes.”

The application starts a Temporal payment workflow.

Temporal executes the transfer through the simulated payment provider.

If the provider times out, Temporal safely retries or checks the transaction status rather than blindly initiating another transfer.

The workflow eventually returns:

“Transfer successful. Transaction reference: TX-10482.”

### Architecture

The workshop will demonstrate a separation between four important responsibilities:

**Gemini — Reasoning Layer**

- Understands the user's intention.
- Determines which available tool should be called.
- Extracts structured parameters from natural language.
- Interprets tool results.
- Communicates the outcome to the user.

**Application — Control Layer**

- Defines which tools the model can access.
- Validates model-generated tool arguments.
- Applies business rules.
- Performs authentication and authorization checks.
- Determines which operations require explicit user confirmation.

**Temporal — Durable Execution Layer**

- Coordinates the payment workflow.
- Persists workflow state.
- Handles retries and timeouts.
- Survives application restarts.
- Handles long-running provider interactions.
- Provides execution history and traceability.

**Payment Provider — External System**

A simulated payment provider introduces realistic scenarios such as:

- successful transactions;
- declined transactions;
- network timeouts;
- delayed processing;
- provider errors;
- ambiguous transaction states.

### Key Takeaways

Participants will learn how to:

1. Build an AI agent that performs real backend operations rather than only generating text.
2. Use Gemini function calling to convert natural-language requests into structured tool invocations.
3. Separate AI reasoning from permission to execute sensitive operations.
4. Design payment tools with strict schemas and validation boundaries.
5. Add human confirmation before high-impact operations such as transferring money.
6. Use idempotency to prevent an AI agent from accidentally initiating duplicate payments.
7. Handle payment-provider timeouts without assuming that a transaction failed.
8. Use Temporal workflows to make agent-triggered operations durable across crashes and network failures.
9. Design retries differently for AI reasoning, internal operations, and external payment APIs.
10. Preserve an auditable history of what the user requested, what the agent proposed, and what the system actually executed.
11. Distinguish between agent orchestration and business-process orchestration.
12. Apply the same architecture to other sensitive agentic systems beyond payments.

### Intended Audience

Backend engineers, AI engineers, cloud engineers, fintech engineers, and developers interested in building agents that interact safely with production systems.

Basic familiarity with APIs and backend development will be useful. Prior experience with Gemini or Temporal will not be required.

### Technologies

- Google Gemini
- Gemini Function Calling
- Temporal
- Go
- REST APIs
- Simulated Payment Provider

### Core Message

**The LLM should decide what it wants to do. Your application should decide what it is allowed to do. Temporal should make sure the approved operation actually completes reliably.**

### Workshop Implementation

The implementation uses **Go 1.27.3**, Google ADK/Gemini, Temporal, and a simulated **Go/PostgreSQL 17** payment service. Participants stay on `main`, edit only `internal/agent/beneficiary.go`, then inspect approval and failure recovery. The agent API and Temporal worker share one Go process; the payment service is the second. Listing/search are supplied before the exercise. All model tool calls use native Temporal Dynamic Activities.

- [Setup and commands](docs/setup.md)
- [Ten-minute beneficiary exercise and complete code](docs/beneficiary-exercise.md)
- [Presenter runbook](docs/presenter.md)
- [Architecture, validation, and REST interfaces](docs/architecture.md)
- [Marp deck and speaker notes](slides/workshop.md)
- [Written follow-up: payment notifications](docs/practice-notifications.md)
- [Written follow-up: scheduled transfers](docs/practice-scheduled-transfers.md)

For this fixed-account local demo, authentication infrastructure is omitted. Validation, allowed-tool policy, PostgreSQL idempotency, and explicit payment confirmation remain enforced. Adding a beneficiary needs no additional confirmation. Reliable completion includes definitive failure or explicitly unresolved status; it does not guarantee payment success.

The starter already lists/searches beneficiaries and sends approved payments to seeded recipients. Complete beneficiary creation with the one-file guide or its supplied replacement file. Use `make restart-agent` after editing; there are no checkpoint settings or branch switches.
