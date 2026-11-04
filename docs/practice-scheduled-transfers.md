# Follow-up: scheduled transfers with durable timers

Start from `practice/02-scheduled-transfers-start`, independently based on main. Proposal parsing and future timestamp validation are supplied. Implement the missing durable wait between approval and payment execution.

The user supplies a future **RFC3339 timestamp with an explicit offset**. Example scripted prompt:

```text
Schedule ₦100 to Ayomide at 2026-11-05T09:00:00+01:00
```

Use a genuinely future time when running; `make lab-check LAB=scheduling` generates one automatically. Relative phrases like “tomorrow morning” are intentionally outside this lab's offline contract.

## Exercise

1. Display recipient, amount and Lagos-local execution time in the proposal.
2. Obtain approval of that exact proposal before scheduling.
3. Set phase to `scheduled` and wait with `workflow.Sleep`, not `time.Sleep` or a goroutine.
4. After the due time, start the normal payment child workflow with the same payment key.
5. Recheck funds inside the payment-service transaction at execution time.

No debit occurs early. Approval expiry applies to the wait for approval, not to the already-approved scheduled timer. If approval arrives after the scheduled time, execute as soon as possible after approval; this is visible in the canonical proposal, and never executes before approval.

## Solution

Replace the exercise block before `ExecuteChildWorkflow` with:

```go
if ScheduleEnabled && s.Proposal.ExecuteAt != "" {
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

Timestamp validation happens inside the preparation Activity; its recorded result is reused on replay. The workflow only parses the validated value and uses Temporal's clock/timer. `ScheduleEnabled` is true on the practice branches and false on main.

## Run and verify

```bash
git switch -c my-schedule practice/02-scheduled-transfers-start
unset WORKSHOP_CHECKPOINT
source scripts/env.sh
make build && make migrate
make start
# Second terminal, same environment:
make lab-check LAB=scheduling
```

The acceptance script creates a near-future proposal, approves it, checks that no debit occurs early, restarts the worker during the timer, then verifies one eventual debit. Also try reducing the account's funds through a separate approved payment before the scheduled payment is due: the scheduled transfer must fail definitively for insufficient funds, without overdrawing.

Complete code is in `practice/02-scheduled-transfers-solution`. Do not put `time.Now`, network calls, or direct database calls into workflow code. No second approval is required at execution because the original approval included the time.
