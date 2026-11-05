package agent

import (
	"context"
	"fmt"
	"iter"
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
	return harnessWithModel(t, ScriptedModel{})
}

func harnessWithModel(t *testing.T, llm model.LLM) (*testsuite.TestWorkflowEnvironment, *int) {
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
	acts, err := googleadk.NewActivities(googleadk.Config{Models: map[string]googleadk.ModelFactory{llm.Name(): func(context.Context, string) (model.LLM, error) { return llm, nil }}})
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

type unavailableModel struct{ attempts *int }

func (unavailableModel) Name() string { return "unavailable" }
func (m unavailableModel) GenerateContent(context.Context, *model.LLMRequest, bool) iter.Seq2[*model.LLMResponse, error] {
	return func(yield func(*model.LLMResponse, error) bool) {
		*m.attempts++
		yield(nil, fmt.Errorf("provider temporarily unavailable"))
	}
}

func TestModelFailureHasBoundedRetries(t *testing.T) {
	if Stage == 0 {
		t.Skip("runtime checkpoint")
	}
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	attempts := 0
	acts, err := googleadk.NewActivities(googleadk.Config{Models: map[string]googleadk.ModelFactory{"unavailable": func(context.Context, string) (model.LLM, error) {
		return unavailableModel{attempts: &attempts}, nil
	}}})
	if err != nil {
		t.Fatal(err)
	}
	defer acts.Close()
	acts.Register(env)
	env.ExecuteWorkflow(AgentWorkflow, Input{Prompt: "List my beneficiaries", Model: "unavailable", Mode: "live"})
	if err := env.GetWorkflowError(); err != nil {
		t.Fatal(err)
	}
	var out State
	if err := env.GetWorkflowResult(&out); err != nil {
		t.Fatal(err)
	}
	if attempts != 3 || out.Phase != "error" || out.Proposal != nil {
		t.Fatalf("model attempts=%d state=%+v", attempts, out)
	}
}

type parallelReadModel struct{ prepare bool }

func (parallelReadModel) Name() string { return "parallel-reads" }
func (m parallelReadModel) GenerateContent(_ context.Context, req *model.LLMRequest, _ bool) iter.Seq2[*model.LLMResponse, error] {
	return func(yield func(*model.LLMResponse, error) bool) {
		for _, content := range req.Contents {
			for _, part := range content.Parts {
				if part.FunctionResponse != nil {
					yield(googleadk.TextResponse("Read both results."), nil)
					return
				}
			}
		}
		response := googleadk.FunctionCallResponse("find", "find_beneficiary", map[string]any{"name": "Ayomide"})
		balance := googleadk.FunctionCallResponse("balance", "check_balance", map[string]any{})
		if m.prepare {
			args := map[string]any{"beneficiary_id": "ayo", "amount_naira": "1"}
			response = googleadk.FunctionCallResponse("first", "prepare_transfer", args)
			balance = googleadk.FunctionCallResponse("second", "prepare_transfer", args)
		}
		response.Content.Parts = append(response.Content.Parts, balance.Content.Parts...)
		yield(response, nil)
	}
}

func TestParallelToolCallsUseTheirOwnWorkflowContext(t *testing.T) {
	if Stage == 0 {
		t.Skip("runtime checkpoint")
	}
	env, debits := harnessWithModel(t, parallelReadModel{})
	env.SetTestTimeout(5 * time.Second)
	env.ExecuteWorkflow(AgentWorkflow, Input{Prompt: "Find Ayomide and check my balance", Model: "parallel-reads", Mode: "live"})
	if err := env.GetWorkflowError(); err != nil {
		t.Fatal(err)
	}
	var out State
	if err := env.GetWorkflowResult(&out); err != nil {
		t.Fatal(err)
	}
	if out.Phase != "completed" || out.Proposal != nil || *debits != 0 || len(out.Tools) != 2 {
		t.Fatalf("parallel tools did not complete: %+v", out)
	}
}

func TestConcurrentPrepareCallsCreateOnlyOneProposal(t *testing.T) {
	if Stage < 4 {
		t.Skip("payment checkpoint not enabled")
	}
	env, debits := harnessWithModel(t, parallelReadModel{prepare: true})
	env.ExecuteWorkflow(AgentWorkflow, Input{Prompt: "Send ₦1 to Ayomide", Model: "parallel-reads", Mode: "live", ApprovalSeconds: 1})
	if err := env.GetWorkflowError(); err != nil {
		t.Fatal(err)
	}
	var out State
	if err := env.GetWorkflowResult(&out); err != nil {
		t.Fatal(err)
	}
	if out.Phase != "expired" || out.Proposal == nil || len(out.Tools) != 1 || out.Tools[0] != "prepare_transfer" || *debits != 0 {
		t.Fatalf("concurrent preparation bypassed one-proposal boundary: %+v debits=%d", out, *debits)
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
