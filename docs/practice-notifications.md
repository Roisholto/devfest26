# Follow-up: durable local payment receipts

Start from `practice/01-notifications-start`, based on the completed core. The payment service provides a **local** receipt sink; no email or SMS is sent. Your job is to call it durably after payment success, while keeping payment and notification outcomes separate.

## Exercise

1. Inspect the successful result in `AgentWorkflow`.
2. Schedule `DeliverReceipt` as an Activity with a bounded retry policy.
3. Use `result.Key` unchanged on every attempt.
4. If delivery exhausts retries, retain successful payment status and report the notification error separately.
5. Prove one debit and one receipt despite transient delivery failures.

The supplied `PaymentActivities.DeliverReceipt` calls `/receipts/{payment_key}?fail_first=2`. The simulated sink persists attempt count; the first two attempts return HTTP 503. The third records one receipt, protected by a primary key on `(payment_key, kind)`. Nothing repeats payment execution.

## Solution

Replace the exercise block after successful payment with:

```go
if NotificationsEnabled && result.Status == "succeeded" {
    nctx := workflow.WithActivityOptions(ctx, toolOptions())
    if err := workflow.ExecuteActivity(nctx, "DeliverReceipt", result.Key).Get(ctx, nil); err != nil {
        s.Error = "payment succeeded; receipt delivery failed: " + err.Error()
    }
}
```

`toolOptions()` limits attempts and total execution time. This Activity is internal, not a model tool. `NotificationsEnabled` is already enabled on the practice branches; it is false on main.

## Run and verify

```bash
git switch -c my-receipts practice/01-notifications-start
unset WORKSHOP_CHECKPOINT
source scripts/env.sh
make build && make migrate
make start
# In a second matching environment terminal:
make lab-check LAB=notifications
```

Expected: successful payment, one additional debit, three notification attempts and one additional receipt. Repeated receipt calls return the existing effect. Test the sink with `make integration`; compare against `practice/01-notifications-solution`.

Do not re-run `PaymentWorkflow` to recover notification failure. A receipt failure must never turn a successful payment into a failed payment or initiate a replacement.
