# Follow-up: durable local receipts

Continue in the same checkout after the beneficiary exercise. The core workflow has no notification flag or notification Activity. The supplied payment service has a local receipt sink and SQL persistence; no email or SMS is sent.

The objective is **three delivery attempts, one receipt and one payment debit** when the sink fails twice. Receipt failure must not change a successful payment to a failed payment or repeat payment execution.

## Add the Activity

In `internal/agent/activities.go`, add:

```go
func (a *PaymentActivities) DeliverReceipt(ctx context.Context, key string) error {
    return activityError(a.Client.Do(ctx, "POST", "/receipts/"+url.PathEscape(key)+"?fail_first=2", "", nil, nil))
}
```

That file already imports `context` and `net/url`. `worker.go` registers `PaymentActivities`, so the new exported method becomes a static internal Activity. Do not advertise it as a model tool.

## Call it after success

In `agent_workflow.go`, insert this block after the final payment answer and before the final return:

```go
if result.Status == "succeeded" {
    s.Phase = "notifying"
    nctx := workflow.WithActivityOptions(ctx, toolOptions())

    if err := workflow.ExecuteActivity(nctx, "DeliverReceipt", result.Key).Get(ctx, nil); err != nil {
        s.Error = "payment succeeded; receipt delivery failed: " + err.Error()
    }

    s.Phase = "succeeded"
}
```

Use the same key for every attempt. The supplied `toolOptions()` bounds retries at three; the provider persists attempts and protects receipt uniqueness.

## Prove it

Stop the agent with Ctrl-C, then run `make agent` again. Save `bin/pay evidence`, approve one payment, wait for its terminal result, and compare evidence:

- `debits` increases by one.
- `receipt_attempts` increases by three.
- `receipts` increases by one.
- Payment status is still `succeeded`.

Call the receipt endpoint again with the same original payment key and verify no additional receipt or debit. To exercise permanent failure, temporarily make `DeliverReceipt` return an error for all three attempts; the workflow must retain payment success and report a separate notification error. These are actual provider/Temporal tests; SDK-only mocks are unit tests.

For an extra exercise, expose an explicitly requested receipt-resend operation that never invokes `PaymentWorkflow` again.
