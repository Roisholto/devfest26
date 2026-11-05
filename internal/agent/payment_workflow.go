package agent

import (
	"time"

	"go.temporal.io/googleadk"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"devfest26/internal/domain"
)

// PaymentWorkflow owns submission and uncertainty; it never calls the model.
func PaymentWorkflow(ctx workflow.Context, p domain.Proposal, scenario string) (domain.Transfer, error) {
	if scenario == "" {
		scenario = "success"
	}

	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout:    6 * time.Second,
		ScheduleToCloseTimeout: 8 * time.Second,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts: 1,
		},
	})
	deadline := workflow.Now(ctx).Add(2 * time.Minute)
	in := domain.SubmitTransfer{
		Key:           p.PaymentKey,
		BeneficiaryID: p.Beneficiary.ID,
		AmountKobo:    p.AmountKobo,
		Currency:      p.Currency,
		Scenario:      scenario,
	}
	latest := domain.Transfer{
		Key:    p.PaymentKey,
		Status: "pending",
		Reason: "outcome unresolved; reconcile the original key",
	}

	for workflow.Now(ctx).Before(deadline) {
		var t domain.Transfer

		err := workflow.ExecuteActivity(ctx, "SubmitTransfer", in).Get(ctx, &t)

		if err == nil {
			latest = t

			if t.Status == "succeeded" || t.Status == "failed" {
				return t, nil
			}
		}

		if err != nil && googleadk.IsNonRetryable(err) {
			return latest, err
		}

		// The lost response may hide a successful commit. Read before resubmitting.
		if err = workflow.ExecuteActivity(ctx, "ReadTransfer", p.PaymentKey).Get(ctx, &t); err == nil {
			latest = t

			if t.Status == "succeeded" || t.Status == "failed" {
				return t, nil
			}
		}

		if err = workflow.Sleep(ctx, 2*time.Second); err != nil {
			return latest, err
		}
	}

	latest.Reason = "automatic reconciliation budget exhausted; use reconcile, not a new transfer"

	return latest, nil
}
