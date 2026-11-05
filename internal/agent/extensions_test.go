package agent

import (
	"context"
	"fmt"
	"testing"
	"time"

	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"

	"devfest26/internal/domain"
)

func TestScheduledPaymentWaitsForDueTime(t *testing.T) {
	if !ScheduleEnabled {
		t.Skip("scheduled-transfer follow-up branch")
	}

	env, debits := harness(t)
	due := time.Now().UTC().Add(time.Hour)
	env.RegisterDelayedCallback(func() {
		v, err := env.QueryWorkflow("state")

		if err != nil {
			t.Error(err)

			return
		}

		var s State

		_ = v.Get(&s)

		if s.Proposal == nil {
			t.Errorf("missing schedule: %+v", s)

			return
		}

		env.UpdateWorkflow("decision", "approve", &testsuite.TestUpdateCallback{
			OnReject: func(err error) { t.Error(err) },
		}, Decision{
			ProposalID: s.Proposal.ID,
			Approve:    true,
		})
	}, time.Second)
	env.RegisterDelayedCallback(func() {
		if *debits != 0 {
			t.Error("payment executed before its due time")
		}
	}, time.Minute)
	env.ExecuteWorkflow(AgentWorkflow, Input{
		Prompt:   "Schedule ₦100 to Ayomide at " + due.Format(time.RFC3339),
		Mode:     "scripted",
		Model:    "scripted",
		Scenario: "success",
	})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatal(err)
	}

	var out State

	_ = env.GetWorkflowResult(&out)

	if out.Phase != "succeeded" || *debits != 1 || env.Now().Before(due.Add(-time.Second)) {
		t.Fatalf("scheduled result %+v debits=%d at %s", out, *debits, env.Now())
	}
}

func TestDeclineNeverRetriesAsSuccess(t *testing.T) {
	var suite testsuite.WorkflowTestSuite

	env := suite.NewTestWorkflowEnvironment()
	env.RegisterWorkflow(PaymentWorkflow)
	attempts := 0
	env.RegisterActivityWithOptions(func(context.Context, domain.SubmitTransfer) (domain.Transfer, error) {
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
	env.ExecuteWorkflow(PaymentWorkflow, domain.Proposal{
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
