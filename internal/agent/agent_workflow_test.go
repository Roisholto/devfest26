package agent

import (
	"context"
	"fmt"
	"iter"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"go.temporal.io/googleadk"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/workflow"
	"google.golang.org/adk/v2/model"

	"devfest26/internal/domain"
	"devfest26/internal/httpx"
)

func harness(t *testing.T) (*testsuite.TestWorkflowEnvironment, *int) {
	return harnessWithModel(t, ScriptedModel{})
}

func harnessWithModel(t *testing.T, llm model.LLM) (*testsuite.TestWorkflowEnvironment, *int) {
	t.Helper()
	beneficiary := domain.Beneficiary{
		ID:            "ayo",
		Name:          "Ayomide",
		BankCode:      "058",
		AccountNumber: "0123454821",
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/beneficiaries":
			httpx.JSON(w, 200, []domain.Beneficiary{beneficiary})
		case "/beneficiaries/ayo":
			httpx.JSON(w, 200, beneficiary)
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
	acts, err := googleadk.NewActivities(googleadk.Config{
		Models: map[string]googleadk.ModelFactory{llm.Name(): func(context.Context, string) (model.LLM, error) { return llm, nil }},
	})

	if err != nil {
		t.Fatal(err)
	}

	acts.Register(env)
	r := &ToolDispatcher{
		Client: httpx.NewClient(srv.URL),
		Limit:  domain.DefaultLimitKobo,
	}
	env.RegisterDynamicActivity(r.Execute, activity.DynamicRegisterOptions{})
	debits := new(int)
	env.RegisterActivityWithOptions(func(_ context.Context, in domain.SubmitTransferRequest) (domain.Transfer, error) {
		*debits++

		return domain.Transfer{
			Key:        in.Key,
			Reference:  "TX-test",
			Status:     "succeeded",
			AmountKobo: in.AmountKobo,
		}, nil
	}, activity.RegisterOptions{
		Name: "SubmitTransfer",
	})
	env.RegisterActivityWithOptions(func(context.Context, string) (domain.Transfer, error) {
		return domain.Transfer{}, fmt.Errorf("not found")
	}, activity.RegisterOptions{
		Name: "ReadTransfer",
	})

	return env, debits
}

func TestApprovalRequiredAndRejectionNoDebit(t *testing.T) {
	for _, approve := range []bool{false, true} {
		t.Run(fmt.Sprint(approve), func(t *testing.T) {
			env, debits := harness(t)
			env.RegisterDelayedCallback(func() {
				v, err := env.QueryWorkflow("state")

				if err != nil {
					t.Error(err)

					return
				}

				var s WorkflowState

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

				d := ApprovalDecision{
					ProposalID: s.Proposal.ID,
					Approve:    approve,
				}
				env.UpdateWorkflow("decision", "decision-1", &testsuite.TestUpdateCallback{
					OnReject: func(err error) { t.Error(err) },
					OnComplete: func(_ any, err error) {
						if err != nil {
							t.Error(err)
						}
					},
				}, d)
			}, time.Second)
			env.ExecuteWorkflow(AgentWorkflow, WorkflowInput{
				Prompt:   "Send ₦10,000 to Ayomide",
				Model:    "scripted",
				Scenario: "success",
			})

			if err := env.GetWorkflowError(); err != nil {
				t.Fatal(err)
			}

			var out WorkflowState

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
	env, debits := harness(t)
	rejected := false
	env.RegisterDelayedCallback(func() {
		env.UpdateWorkflow("decision", "bad", &testsuite.TestUpdateCallback{
			OnReject: func(error) { rejected = true },
		}, ApprovalDecision{
			ProposalID: "made-up",
			Approve:    true,
		})
	}, time.Second)
	env.ExecuteWorkflow(AgentWorkflow, WorkflowInput{
		Prompt:          "Send ₦10,000 to Ayomide",
		Model:           "scripted",
		ApprovalSeconds: 2,
	})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatal(err)
	}

	var out WorkflowState

	_ = env.GetWorkflowResult(&out)

	if out.Phase != "expired" || *debits != 0 || !rejected {
		t.Fatalf("state=%+v debits=%d rejected=%v", out, *debits, rejected)
	}
}

func TestNativeDynamicToolValidation(t *testing.T) {
	env, _ := harness(t)
	env.ExecuteWorkflow(func(ctx workflow.Context) (map[string]any, error) {
		ctx = workflow.WithActivityOptions(ctx, toolOptions())

		var out map[string]any

		err := workflow.ExecuteActivity(ctx, "search_beneficiaries", map[string]any{"query": 123}).Get(ctx, &out)

		return out, err
	})

	if err := env.GetWorkflowError(); err == nil || !strings.Contains(err.Error(), "query must be a string") {
		t.Fatalf("invalid dynamic tool arguments were not rejected: %v", err)
	}
}

func TestSuccessfulReadToolsNeedNoApproval(t *testing.T) {
	env, debits := harness(t)
	env.ExecuteWorkflow(AgentWorkflow, WorkflowInput{
		Prompt: "Search beneficiaries for Ayo",
		Model:  "scripted",
	})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatal(err)
	}

	var out WorkflowState

	_ = env.GetWorkflowResult(&out)

	if out.Phase != "completed" || out.Proposal != nil || *debits != 0 || len(out.Tools) != 1 || out.Tools[0] != "search_beneficiaries" {
		t.Fatalf("unexpected result %+v", out)
	}
}

// unavailableModel counts failing model calls to verify the Activity retry limit.
type unavailableModel struct{ attempts *int }

func (unavailableModel) Name() string { return "unavailable" }

func (m unavailableModel) GenerateContent(context.Context, *model.LLMRequest, bool) iter.Seq2[*model.LLMResponse, error] {
	return func(yield func(*model.LLMResponse, error) bool) {
		*m.attempts++
		yield(nil, fmt.Errorf("provider temporarily unavailable"))
	}
}

func TestModelFailureHasBoundedRetries(t *testing.T) {
	var suite testsuite.WorkflowTestSuite

	env := suite.NewTestWorkflowEnvironment()
	attempts := 0
	acts, err := googleadk.NewActivities(googleadk.Config{
		Models: map[string]googleadk.ModelFactory{"unavailable": func(context.Context, string) (model.LLM, error) {
			return unavailableModel{
				attempts: &attempts,
			}, nil
		}},
	})

	if err != nil {
		t.Fatal(err)
	}

	defer acts.Close()

	acts.Register(env)
	env.ExecuteWorkflow(AgentWorkflow, WorkflowInput{
		Prompt: "List my beneficiaries",
		Model:  "unavailable",
	})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatal(err)
	}

	var out WorkflowState

	if err := env.GetWorkflowResult(&out); err != nil {
		t.Fatal(err)
	}

	if attempts != 3 || out.Phase != "error" || out.Proposal != nil {
		t.Fatalf("model attempts=%d state=%+v", attempts, out)
	}
}

// parallelToolCallModel emits two calls in one turn to exercise concurrent
// tool waits and, when prepare is set, the single-proposal guard.
type parallelToolCallModel struct{ prepare bool }

func (parallelToolCallModel) Name() string { return "parallel-tool-calls" }

func (m parallelToolCallModel) GenerateContent(_ context.Context, req *model.LLMRequest, _ bool) iter.Seq2[*model.LLMResponse, error] {
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
	env, debits := harnessWithModel(t, parallelToolCallModel{})
	env.SetTestTimeout(5 * time.Second)
	env.ExecuteWorkflow(AgentWorkflow, WorkflowInput{
		Prompt: "Find Ayomide and check my balance",
		Model:  "parallel-tool-calls",
	})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatal(err)
	}

	var out WorkflowState

	if err := env.GetWorkflowResult(&out); err != nil {
		t.Fatal(err)
	}

	if out.Phase != "completed" || out.Proposal != nil || *debits != 0 || len(out.Tools) != 2 {
		t.Fatalf("parallel tools did not complete: %+v", out)
	}
}

func TestConcurrentPrepareCallsCreateOnlyOneProposal(t *testing.T) {
	env, debits := harnessWithModel(t, parallelToolCallModel{
		prepare: true,
	})
	env.ExecuteWorkflow(AgentWorkflow, WorkflowInput{
		Prompt:          "Send ₦1 to Ayomide",
		Model:           "parallel-tool-calls",
		ApprovalSeconds: 1,
	})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatal(err)
	}

	var out WorkflowState

	if err := env.GetWorkflowResult(&out); err != nil {
		t.Fatal(err)
	}

	if out.Phase != "expired" || out.Proposal == nil || len(out.Tools) != 1 || out.Tools[0] != "prepare_transfer" || *debits != 0 {
		t.Fatalf("concurrent preparation bypassed one-proposal boundary: %+v debits=%d", out, *debits)
	}
}
