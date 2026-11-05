# Follow-up: a durable scheduled payment

Continue in the same checkout. Scheduling is intentionally absent from the core workflow, tool schema and scripted model. This practice adds one explicit RFC3339 execution time, validates it at the Activity boundary and waits durably after approval.

Use Gemini for the natural-language scheduling prompt. First complete the field/schema changes below; then use a future Lagos-offset timestamp such as `Send ₦100 to John at 2026-11-06T09:00:00+01:00`. Generate a genuinely future timestamp when practising.

## Carry and validate the time

Add this field to both `domain.Proposal` and `agent.PrepareArgs`:

```go
ExecuteAt string `json:"execute_at,omitempty"`
```

Add `"execute_at"` to the `prepare_transfer` specification's `Fields` slice in `tools.go`; leave it optional in `Required`. Extend that tool's description and the agent instruction: when the user requests scheduling, pass an explicit RFC3339 timestamp with timezone and never invent a missing time.

Import `time` in `tools.go`. After validating the amount in the preparation Activity, validate the optional timestamp:

```go
if in.ExecuteAt != "" {
    due, err := time.Parse(time.RFC3339, in.ExecuteAt)

    if err != nil || !due.After(time.Now()) {
        return nil, domain.Invalid("execute_at must be a future RFC3339 timestamp with timezone")
    }
}
```

Include `ExecuteAt: in.ExecuteAt` on its own line in the returned `domain.Proposal` literal. All network I/O, wall-clock validation and model calls remain outside workflow code; replay reuses the recorded proposal.

## Include the time in approval

In `agent_workflow.go`, after constructing the canonical confirmation answer, add:

```go
if s.Proposal.ExecuteAt != "" {
    due, _ := time.Parse(time.RFC3339, s.Proposal.ExecuteAt)
    s.Answer += " Execute at " + due.In(time.FixedZone("Africa/Lagos", 3600)).Format(time.RFC3339) + " (Africa/Lagos)."
}
```

The user must approve the exact recipient, amount and time. Approval expiry covers waiting for the decision. If approval arrives after the specified time, execute as soon as available after approval; never execute before approval.

## Wait before starting the child

Immediately before `workflow.WithChildOptions` in `agent_workflow.go`, add:

```go
if s.Proposal.ExecuteAt != "" {
    due, _ := time.Parse(time.RFC3339, s.Proposal.ExecuteAt)
    s.Phase = "scheduled"

    if delay := due.Sub(workflow.Now(ctx)); delay > 0 {
        if err := workflow.Sleep(ctx, delay); err != nil {
            return s, err
        }
    }

    s.Phase = "executing"
}
```

This uses Temporal's timer, not `time.Sleep` or a goroutine. The existing payment child and provider recheck funds at execution, using the same payment key.

## Prove it with real services

Stop the agent with Ctrl-C, then run `make agent` again. Request a payment a few minutes ahead, inspect its canonical time, then approve. Verify `scheduled` and no debit, crash/restart the agent, and retrieve the same request. After the due time there must be one debit, one reference and no repeat model calls during recovery.

Also spend enough funds through a separate approved payment before the due time: the scheduled payment should fail definitively for insufficient funds. Test missing/invalid/past timestamps and withheld/rejected approval. Add cancellation as a further exercise, with an explicit rule for when cancellation is too late.
