package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"devfest26/internal/agent"
	"devfest26/internal/domain"
	"devfest26/internal/httpx"
)

// These unit tests script only the CLI's HTTP boundary. Real service coverage
// lives in scripts/check-demo.py and uses Gemini, Temporal and PostgreSQL.
func cliServer(t *testing.T, handler http.HandlerFunc) {
	t.Helper()

	s := httptest.NewServer(handler)
	t.Cleanup(s.Close)
	t.Setenv("API_ADDRESS", strings.TrimPrefix(s.URL, "http://"))
}

func callCLI(t *testing.T, args ...string) workflowResponse {
	t.Helper()

	var output bytes.Buffer

	if err := run(context.Background(), args, &output); err != nil {
		t.Fatalf("CLI failed: %v; output: %s", err, &output)
	}

	var out workflowResponse

	if err := json.Unmarshal(output.Bytes(), &out); err != nil {
		t.Fatal(err)
	}

	return out
}

func TestAskReturnsAnswerInSameCommand(t *testing.T) {
	var submitted atomic.Int32
	var queried atomic.Int32

	cliServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "POST /requests":
			submitted.Add(1)

			if r.Header.Get("Idempotency-Key") != "one-request" {
				t.Error("request key not preserved")
			}

			httpx.JSON(w, 202, map[string]string{"request_id": "test-request", "workflow_id": "test-request", "model": "test-model"})
		case "GET /requests/test-request":
			state := agent.WorkflowState{
				Phase:  "reasoning",
				Model:  "test-model",
				Answer: "Beneficiaries: John, Zainab",
			}

			if queried.Add(1) > 1 {
				state.Phase = "completed"
			}

			httpx.JSON(w, 200, state)
		default:
			t.Error("unexpected request:", r.Method, r.URL.Path)
			w.WriteHeader(404)
		}
	})

	out := callCLI(t, "ask", "--key", "one-request", "--timeout", "3s", "List my beneficiaries")

	if out.WorkflowState == nil || out.Phase != "completed" || out.Answer == "" || out.RequestID != "test-request" || out.RequestKey != "one-request" || out.WaitTimedOut {
		t.Fatalf("missing inline answer or identity: %+v", out)
	}

	if submitted.Load() != 1 || queried.Load() != 2 {
		t.Fatal("waiting resubmitted the request or stopped before the answer")
	}
}

func TestAskStopsAtExplicitApprovalBoundary(t *testing.T) {
	cliServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" && r.URL.Path == "/requests" {
			httpx.JSON(w, 202, map[string]string{"request_id": "proposal", "workflow_id": "proposal"})

			return
		}

		if r.Method != "GET" || r.URL.Path != "/requests/proposal" {
			t.Error("ask must not submit a decision")
		}

		httpx.JSON(w, 200, agent.WorkflowState{
			Phase: "awaiting_approval",
			Proposal: &domain.TransferProposal{
				ID: "proposal-id",
			},
		})
	})

	out := callCLI(t, "ask", "--timeout", "1s", "Send money to John")

	if out.WorkflowState == nil || out.Phase != "awaiting_approval" || out.Proposal == nil || out.WaitTimedOut {
		t.Fatalf("expected the proposal without implicit approval: %+v", out)
	}
}

func TestDeadlineCancelsInFlightQueryAndWaitResumesSameRequest(t *testing.T) {
	var ready atomic.Bool
	var submitted atomic.Int32
	queryCanceled := make(chan struct{}, 1)

	cliServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" && r.URL.Path == "/requests" {
			submitted.Add(1)
			httpx.JSON(w, 202, map[string]string{"request_id": "durable", "workflow_id": "durable", "model": "test-model"})

			return
		}

		if r.Method != "GET" || r.URL.Path != "/requests/durable" {
			t.Error("unexpected operation; timeout must not cancel the workflow")
		}

		if !ready.Load() {
			<-r.Context().Done()
			queryCanceled <- struct{}{}

			return
		}

		httpx.JSON(w, 200, agent.WorkflowState{
			Phase:  "completed",
			Answer: "Recovered answer",
		})
	})

	started := time.Now()
	pending := callCLI(t, "ask", "--key", "durable-key", "--timeout", "100ms", "List my beneficiaries")

	if time.Since(started) > time.Second {
		t.Fatal("command deadline did not bound an in-flight HTTP request")
	}

	if pending.WorkflowState != nil || !pending.WaitTimedOut || pending.RequestID != "durable" || pending.RequestKey != "durable-key" || pending.ResumeCommand != "go run ./cmd/pay wait durable" {
		t.Fatalf("timeout response is not resumable: %+v", pending)
	}

	select {
	case <-queryCanceled:
	case <-time.After(time.Second):
		t.Fatal("HTTP request context was not canceled")
	}

	ready.Store(true)
	result := callCLI(t, "wait", "--timeout", "1s", pending.RequestID)

	if result.WorkflowState == nil || result.Answer != "Recovered answer" || result.RequestID != pending.RequestID || submitted.Load() != 1 {
		t.Fatalf("wait did not resume the original request: %+v", result)
	}
}

func TestDecisionsReturnOutcomeWithoutAnotherWaitCommand(t *testing.T) {
	for _, approve := range []bool{true, false} {
		name, phase := "reject", "rejected"

		if approve {
			name, phase = "approve", "succeeded"
		}

		t.Run(name, func(t *testing.T) {
			var decided atomic.Bool

			cliServer(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method == "POST" && r.URL.Path == "/requests/payment/decision" {
					var decision agent.ApprovalDecision

					if err := json.NewDecoder(r.Body).Decode(&decision); err != nil {
						t.Error(err)
					}

					if decision.Approve != approve || decision.ProposalID != "proposal-id" || r.Header.Get("Idempotency-Key") == "" {
						t.Error("decision or its stable key changed")
					}

					decided.Store(true)
					httpx.JSON(w, 200, map[string]string{"decision": "accepted"})

					return
				}

				if r.Method != "GET" || r.URL.Path != "/requests/payment" {
					t.Error("unexpected request")
				}

				state := agent.WorkflowState{
					Phase: "awaiting_approval",
					Proposal: &domain.TransferProposal{
						ID: "proposal-id",
					},
				}

				if decided.Load() {
					state.Phase = phase
				}

				httpx.JSON(w, 200, state)
			})

			out := callCLI(t, name, "--timeout", "1s", "payment")

			if out.WorkflowState == nil || out.Phase != phase || out.DecisionStatus != "accepted" || out.DecisionKey == "" || out.WaitTimedOut {
				t.Fatalf("decision did not return its outcome: %+v", out)
			}
		})
	}
}

func TestWaitReturnsLastObservedStateAtDeadline(t *testing.T) {
	cliServer(t, func(w http.ResponseWriter, r *http.Request) {
		httpx.JSON(w, 200, agent.WorkflowState{
			Phase: "executing",
		})
	})

	out := callCLI(t, "wait", "--terminal", "--timeout", "100ms", "payment")

	if out.WorkflowState == nil || out.Phase != "executing" || !out.WaitTimedOut || out.ResumeCommand != "go run ./cmd/pay wait --terminal payment" {
		t.Fatalf("missing last observed state or terminal resume command: %+v", out)
	}
}

func TestWaitDoesNotRetryPermanentErrors(t *testing.T) {
	var queries atomic.Int32

	cliServer(t, func(w http.ResponseWriter, r *http.Request) {
		queries.Add(1)
		httpx.Error(w, domain.NotFound("request not found"))
	})

	var output bytes.Buffer

	err := run(context.Background(), []string{"wait", "--timeout", "2s", "missing"}, &output)

	var problem *domain.Problem

	if !errors.As(err, &problem) || problem.Code != "not_found" || queries.Load() != 1 {
		t.Fatalf("permanent error was swallowed or retried: %v; queries=%d", err, queries.Load())
	}
}

func TestTimeoutMustBePositive(t *testing.T) {
	var output bytes.Buffer

	if err := run(context.Background(), []string{"ask", "--timeout", "0", "hello"}, &output); err == nil {
		t.Fatal("accepted an unbounded timeout")
	}
}

func TestApprovalDeadlineRetainsAcceptedDecisionWithoutResubmission(t *testing.T) {
	var decisions atomic.Int32

	cliServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" && r.URL.Path == "/requests/payment/decision" {
			decisions.Add(1)
			httpx.JSON(w, 200, map[string]string{"decision": "accepted"})

			return
		}

		state := agent.WorkflowState{
			Phase: "awaiting_approval",
			Proposal: &domain.TransferProposal{
				ID: "proposal-id",
			},
		}

		if decisions.Load() > 0 {
			state.Phase = "executing"
		}

		httpx.JSON(w, 200, state)
	})

	out := callCLI(t, "approve", "--timeout", "100ms", "payment")

	if out.WorkflowState == nil || out.Phase != "executing" || !out.WaitTimedOut || out.DecisionStatus != "accepted" || out.DecisionKey == "" || out.ResumeCommand != "go run ./cmd/pay wait --terminal payment" || decisions.Load() != 1 {
		t.Fatalf("lost or repeated an accepted decision at the deadline: %+v", out)
	}
}

func TestMissingSubmissionAcknowledgementRetainsRequestKey(t *testing.T) {
	cliServer(t, func(w http.ResponseWriter, r *http.Request) {
		// Consume the submission before simulating a lost acknowledgement.
		// This also lets net/http observe the client's closed connection.
		_, _ = io.Copy(io.Discard, r.Body)

		<-r.Context().Done()
	})

	var output bytes.Buffer

	err := run(context.Background(), []string{"ask", "--key", "same-operation", "--timeout", "100ms", "List beneficiaries"}, &output)

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("unknown submission should retain its acknowledgement error: %v", err)
	}

	var out workflowResponse

	if err := json.Unmarshal(output.Bytes(), &out); err != nil {
		t.Fatal(err)
	}

	if out.RequestKey != "same-operation" || out.RequestID != "" || out.WaitTimedOut {
		t.Fatalf("missing safe retry key or invented request acceptance: %+v", out)
	}
}
