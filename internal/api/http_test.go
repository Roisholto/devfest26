package api

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/mock"
	commonpb "go.temporal.io/api/common/v1"
	"go.temporal.io/api/serviceerror"
	workflowpb "go.temporal.io/api/workflow/v1"
	"go.temporal.io/api/workflowservice/v1"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/converter"
	"go.temporal.io/sdk/mocks"

	"devfest26/internal/agent"
	"devfest26/internal/config"
	"devfest26/internal/domain"
)

func TestApprovalRequiresExplicitBoolean(t *testing.T) {
	s := &Server{
		Config: config.Config{
			Prefix: "devfest26-workshop",
		},
	}

	for _, body := range []string{`{"proposal_id":"one"}`, `{"proposal_id":"one","approve":"yes"}`, `{"proposal_id":"one","approve":null}`, `{"approve":true}`, `{"proposal_id":"one","approve":true,"amount":10}`} {
		r := httptest.NewRequest("POST", "/requests/devfest26-workshop-test/decision", strings.NewReader(body))
		r.Header.Set("Idempotency-Key", "test")
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)

		if w.Code != 400 {
			t.Fatalf("accepted invalid decision %s: %d", body, w.Code)
		}
	}
}

func TestDuplicateRequestSurfacesConflictAndComparesPayload(t *testing.T) {
	for _, same := range []bool{false, true} {
		c := &mocks.Client{}
		c.On("ExecuteWorkflow", mock.Anything, mock.MatchedBy(func(o client.StartWorkflowOptions) bool { return o.WorkflowExecutionErrorWhenAlreadyStarted }), mock.Anything, mock.Anything).Return(nil, &serviceerror.WorkflowExecutionAlreadyStarted{})
		old := Request{
			Prompt:   "Send ₦100 to Ayomide",
			Scenario: "success",
		}
		hash := domain.Fingerprint(old)

		if !same {
			hash = "another-payload"
		}

		payload, _ := converter.GetDefaultDataConverter().ToPayload(hash)
		c.On("DescribeWorkflowExecution", mock.Anything, mock.Anything, "").Return(&workflowservice.DescribeWorkflowExecutionResponse{
			WorkflowExecutionInfo: &workflowpb.WorkflowExecutionInfo{
				Memo: &commonpb.Memo{
					Fields: map[string]*commonpb.Payload{"request_fingerprint": payload},
				},
			},
		}, nil)
		s := &Server{
			Temporal: c,
			Config: config.Config{
				Prefix: "devfest26-workshop",
			},
		}
		r := httptest.NewRequest("POST", "/requests", strings.NewReader(`{"prompt":"Send ₦100 to Ayomide"}`)).WithContext(context.Background())
		r.Header.Set("Idempotency-Key", "same-key")
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		want := 409

		if same {
			want = 202
		}

		if w.Code != want {
			t.Fatalf("same=%v: status=%d body=%s", same, w.Code, w.Body.String())
		}

		c.AssertExpectations(t)
	}
}

func TestEnvironmentIsolation(t *testing.T) {
	s := &Server{
		Config: config.Config{
			Prefix: "devfest26-workshop",
		},
	}
	r := httptest.NewRequest("GET", "/requests/another-workshop-other", nil)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)

	if w.Code != 404 {
		t.Fatalf("cross-environment request returned %d", w.Code)
	}
}

func TestReusedUpdateIDCannotHideChangedDecision(t *testing.T) {
	for _, same := range []bool{false, true} {
		c := &mocks.Client{}
		h := &mocks.WorkflowUpdateHandle{}
		original := agent.Decision{
			ProposalID: "one",
			Approve:    false,
		}
		h.On("Get", mock.Anything, mock.Anything).Run(func(a mock.Arguments) { *a.Get(1).(*string) = domain.Fingerprint(original) }).Return(nil)
		c.On("UpdateWorkflow", mock.Anything, mock.Anything).Return(h, nil)
		s := &Server{
			Temporal: c,
			Config: config.Config{
				Prefix: "devfest26-workshop",
			},
		}
		body := `{"proposal_id":"one","approve":true}`

		if same {
			body = `{"proposal_id":"one","approve":false}`
		}

		r := httptest.NewRequest("POST", "/requests/devfest26-workshop-test/decision", strings.NewReader(body))
		r.Header.Set("Idempotency-Key", "reused-update-id")
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		want := 409

		if same {
			want = 200
		}

		if w.Code != want {
			t.Fatalf("same=%v: %d %s", same, w.Code, w.Body.String())
		}
	}
}
