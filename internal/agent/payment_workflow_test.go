package agent

import (
	"context"
	"fmt"
	"testing"

	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"

	"devfest26/internal/domain"
)

func TestDeclineNeverRetriesAsSuccess(t *testing.T) {
	var suite testsuite.WorkflowTestSuite

	env := suite.NewTestWorkflowEnvironment()
	env.RegisterWorkflow(PaymentWorkflow)
	attempts := 0
	env.RegisterActivityWithOptions(func(context.Context, domain.SubmitTransferRequest) (domain.Transfer, error) {
		attempts++

		return domain.Transfer{
			Key:    "stable",
			Status: "failed",
			Reason: "decline",
		}, nil
	}, activity.RegisterOptions{
		Name: "SubmitTransfer",
	})
	env.RegisterActivityWithOptions(func(context.Context, string) (domain.Transfer, error) {
		return domain.Transfer{}, fmt.Errorf("should not reconcile terminal decline")
	}, activity.RegisterOptions{
		Name: "ReadTransfer",
	})
	env.ExecuteWorkflow(PaymentWorkflow, domain.TransferProposal{
		PaymentKey: "stable",
	}, "decline")

	if err := env.GetWorkflowError(); err != nil {
		t.Fatal(err)
	}

	var out domain.Transfer

	_ = env.GetWorkflowResult(&out)

	if out.Status != "failed" || attempts != 1 {
		t.Fatalf("attempts=%d result=%+v", attempts, out)
	}
}

func TestPaymentReconcilesBeforeResubmitting(t *testing.T) {
	var suite testsuite.WorkflowTestSuite

	env := suite.NewTestWorkflowEnvironment()
	env.RegisterWorkflow(PaymentWorkflow)
	submissions := 0
	env.RegisterActivityWithOptions(func(context.Context, domain.SubmitTransferRequest) (domain.Transfer, error) {
		submissions++

		return domain.Transfer{}, fmt.Errorf("response lost after commit")
	}, activity.RegisterOptions{
		Name: "SubmitTransfer",
	})
	env.RegisterActivityWithOptions(func(_ context.Context, key string) (domain.Transfer, error) {
		return domain.Transfer{
			Key:       key,
			Reference: "TX-existing",
			Status:    "succeeded",
		}, nil
	}, activity.RegisterOptions{
		Name: "ReadTransfer",
	})
	env.ExecuteWorkflow(PaymentWorkflow, domain.TransferProposal{
		PaymentKey: "stable",
		Beneficiary: domain.Beneficiary{
			ID: "ayo",
		},
		AmountKobo: 1000000,
		Currency:   "NGN",
	}, "lost-response")

	if err := env.GetWorkflowError(); err != nil {
		t.Fatal(err)
	}

	var out domain.Transfer

	_ = env.GetWorkflowResult(&out)

	if out.Reference != "TX-existing" || submissions != 1 {
		t.Fatalf("submissions=%d result=%+v", submissions, out)
	}
}

func TestPaymentBoundsReconciliation(t *testing.T) {
	var suite testsuite.WorkflowTestSuite

	env := suite.NewTestWorkflowEnvironment()
	env.RegisterWorkflow(PaymentWorkflow)
	env.RegisterActivityWithOptions(func(context.Context, domain.SubmitTransferRequest) (domain.Transfer, error) {
		return domain.Transfer{}, fmt.Errorf("offline")
	}, activity.RegisterOptions{
		Name: "SubmitTransfer",
	})
	env.RegisterActivityWithOptions(func(context.Context, string) (domain.Transfer, error) {
		return domain.Transfer{}, fmt.Errorf("offline")
	}, activity.RegisterOptions{
		Name: "ReadTransfer",
	})
	env.ExecuteWorkflow(PaymentWorkflow, domain.TransferProposal{
		PaymentKey: "stable",
		Beneficiary: domain.Beneficiary{
			ID: "ayo",
		},
		AmountKobo: 1000000,
		Currency:   "NGN",
	}, "success")

	if err := env.GetWorkflowError(); err != nil {
		t.Fatal(err)
	}

	var out domain.Transfer

	_ = env.GetWorkflowResult(&out)

	if out.Status != "pending" || out.Key != "stable" {
		t.Fatalf("unexpected %+v", out)
	}
}
