package agent

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"go.temporal.io/googleadk"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"
	"google.golang.org/adk/v2/model"

	"devfest26/internal/domain"
	"devfest26/internal/httpx"
)

func harness(t *testing.T) (*testsuite.TestWorkflowEnvironment, *int) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/beneficiaries":
			httpx.JSON(w, 200, []domain.Beneficiary{{ID: "ayo", Name: "Ayomide", BankCode: "058", AccountNumber: "0123454821"}})
		case "/balance":
			httpx.JSON(w, 200, map[string]any{"balance_kobo": 10000000})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	env.RegisterWorkflow(AgentWorkflow)
	env.RegisterWorkflow(PaymentWorkflow)
	acts, err := googleadk.NewActivities(googleadk.Config{Models: map[string]googleadk.ModelFactory{"scripted": func(context.Context, string) (model.LLM, error) { return ScriptedModel{}, nil }}})
	if err != nil {
		t.Fatal(err)
	}
	acts.Register(env)
	r := &Router{Client: httpx.NewClient(srv.URL), Limit: domain.DefaultLimitKobo}
	env.RegisterDynamicActivity(r.Execute, activity.DynamicRegisterOptions{})
	debits := new(int)
	env.RegisterActivityWithOptions(func(_ context.Context, in domain.SubmitTransfer) (domain.Transfer, error) {
		*debits++
		return domain.Transfer{Key: in.Key, Reference: "TX-test", Status: "succeeded", AmountKobo: in.AmountKobo}, nil
	}, activity.RegisterOptions{Name: "SubmitTransfer"})
	env.RegisterActivityWithOptions(func(context.Context, string) (domain.Transfer, error) {
		return domain.Transfer{}, fmt.Errorf("not found")
	}, activity.RegisterOptions{Name: "ReadTransfer"})
	env.RegisterActivityWithOptions(func(context.Context, string) error { return nil }, activity.RegisterOptions{Name: "DeliverReceipt"})
	return env, debits
}
func TestApprovalRequiredAndRejectionNoDebit(t *testing.T) {
	if Stage < 4 {
		t.Skip("payment checkpoint not enabled")
	}
	for _, approve := range []bool{false, true} {
		t.Run(fmt.Sprint(approve), func(t *testing.T) {
			env, debits := harness(t)
			env.RegisterDelayedCallback(func() {
				v, err := env.QueryWorkflow("state")
				if err != nil {
					t.Error(err)
					return
				}
				var s State
				if err = v.Get(&s); err != nil {
					t.Error(err)
					return
				}
				if s.Phase != "awaiting_approval" || s.Proposal == nil {
					t.Errorf("not awaiting approval: %+v", s)
					return
				}
				if *debits != 0 {
					t.Error("debited before approval")
				}
				d := Decision{ProposalID: s.Proposal.ID, Approve: approve}
				env.UpdateWorkflow("decision", "decision-1", &testsuite.TestUpdateCallback{OnReject: func(err error) { t.Error(err) }, OnComplete: func(_ any, err error) {
					if err != nil {
						t.Error(err)
					}
				}}, d)
			}, time.Second)
			env.ExecuteWorkflow(AgentWorkflow, Input{Prompt: "Send ₦10,000 to Ayomide", Model: "scripted", Mode: "scripted", Scenario: "success"})
			if err := env.GetWorkflowError(); err != nil {
				t.Fatal(err)
			}
			var out State
			if err := env.GetWorkflowResult(&out); err != nil {
				t.Fatal(err)
			}
			want := "rejected"
			n := 0
			if approve {
				want = "succeeded"
				n = 1
			}
			if out.Phase != want || *debits != n {
				t.Fatalf("state=%+v debits=%d", out, *debits)
			}
		})
	}
}
func TestExpiryAndStaleApproval(t *testing.T) {
	if Stage < 4 {
		t.Skip("payment checkpoint not enabled")
	}
	env, debits := harness(t)
	rejected := false
	env.RegisterDelayedCallback(func() {
		env.UpdateWorkflow("decision", "bad", &testsuite.TestUpdateCallback{OnReject: func(error) { rejected = true }}, Decision{ProposalID: "made-up", Approve: true})
	}, time.Second)
	env.ExecuteWorkflow(AgentWorkflow, Input{Prompt: "Send ₦10,000 to Ayomide", Model: "scripted", Mode: "scripted", ApprovalSeconds: 2})
	if err := env.GetWorkflowError(); err != nil {
		t.Fatal(err)
	}
	var out State
	_ = env.GetWorkflowResult(&out)
	if out.Phase != "expired" || *debits != 0 || !rejected {
		t.Fatalf("state=%+v debits=%d rejected=%v", out, *debits, rejected)
	}
}
func TestNativeDynamicToolValidation(t *testing.T) {
	if Stage < 3 {
		t.Skip("beneficiary checkpoint not enabled")
	}
	env, _ := harness(t)
	env.ExecuteWorkflow(AgentWorkflow, Input{Prompt: "Add Ayomide at GTBank, account 123", Model: "scripted", Mode: "scripted"})
	if err := env.GetWorkflowError(); err != nil {
		t.Fatal(err)
	}
	var out State
	_ = env.GetWorkflowResult(&out)
	if out.Phase != "error" || out.Proposal != nil {
		t.Fatalf("invalid tool accepted: %+v", out)
	}
}
func TestSuccessfulReadToolsNeedNoApproval(t *testing.T) {
	if Stage == 0 {
		t.Skip("runtime checkpoint")
	}
	env, debits := harness(t)
	env.ExecuteWorkflow(AgentWorkflow, Input{Prompt: "Search beneficiaries for Ayo", Model: "scripted", Mode: "scripted"})
	if err := env.GetWorkflowError(); err != nil {
		t.Fatal(err)
	}
	var out State
	_ = env.GetWorkflowResult(&out)
	if out.Phase != "completed" || out.Proposal != nil || *debits != 0 || len(out.Tools) != 1 || out.Tools[0] != "search_beneficiaries" {
		t.Fatalf("unexpected result %+v", out)
	}
}
func TestPaymentReconcilesBeforeResubmitting(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	env.RegisterWorkflow(PaymentWorkflow)
	submissions := 0
	env.RegisterActivityWithOptions(func(context.Context, domain.SubmitTransfer) (domain.Transfer, error) {
		submissions++
		return domain.Transfer{}, fmt.Errorf("response lost after commit")
	}, activity.RegisterOptions{Name: "SubmitTransfer"})
	env.RegisterActivityWithOptions(func(_ context.Context, key string) (domain.Transfer, error) {
		return domain.Transfer{Key: key, Reference: "TX-existing", Status: "succeeded"}, nil
	}, activity.RegisterOptions{Name: "ReadTransfer"})
	env.ExecuteWorkflow(PaymentWorkflow, domain.Proposal{PaymentKey: "stable", Beneficiary: domain.Beneficiary{ID: "ayo"}, AmountKobo: 1000000, Currency: "NGN"}, "lost-response")
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
	env.RegisterActivityWithOptions(func(context.Context, domain.SubmitTransfer) (domain.Transfer, error) {
		return domain.Transfer{}, fmt.Errorf("offline")
	}, activity.RegisterOptions{Name: "SubmitTransfer"})
	env.RegisterActivityWithOptions(func(context.Context, string) (domain.Transfer, error) {
		return domain.Transfer{}, fmt.Errorf("offline")
	}, activity.RegisterOptions{Name: "ReadTransfer"})
	env.ExecuteWorkflow(PaymentWorkflow, domain.Proposal{PaymentKey: "stable", Beneficiary: domain.Beneficiary{ID: "ayo"}, AmountKobo: 1000000, Currency: "NGN"}, "success")
	if err := env.GetWorkflowError(); err != nil {
		t.Fatal(err)
	}
	var out domain.Transfer
	_ = env.GetWorkflowResult(&out)
	if out.Status != "pending" || out.Key != "stable" {
		t.Fatalf("unexpected %+v", out)
	}
}
