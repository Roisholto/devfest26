package application

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"go.temporal.io/api/enums/v1"
	"go.temporal.io/api/serviceerror"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/converter"

	"devfest26/internal/agent"
	"devfest26/internal/config"
	"devfest26/internal/domain"
	"devfest26/internal/httpx"
)

type Server struct {
	Temporal client.Client
	Config   config.Config
	Payment  *httpx.Client
}
type Request struct {
	Prompt          string `json:"prompt"`
	Scenario        string `json:"scenario,omitempty"`
	ApprovalSeconds int    `json:"approval_seconds,omitempty"`
}

func (s *Server) Handler() http.Handler {
	m := http.NewServeMux()
	m.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		if _, err := s.Temporal.CheckHealth(r.Context(), &client.CheckHealthRequest{}); err != nil {
			httpx.Error(w, err)
			return
		}
		httpx.JSON(w, 200, map[string]any{"status": "ready", "instance": s.Config.Instance, "model_mode": s.Config.Mode, "checkpoint": s.Config.Checkpoint})
	})
	m.HandleFunc("POST /requests", s.submit)
	m.HandleFunc("GET /requests/{id}", s.state)
	m.HandleFunc("POST /requests/{id}/decision", s.decision)
	m.HandleFunc("GET /transactions/{reference}", s.transaction)
	m.HandleFunc("POST /transactions/{reference}/reconcile", s.transaction)
	return m
}
func (s *Server) submit(w http.ResponseWriter, r *http.Request) {
	var in Request
	if err := httpx.Decode(w, r, &in); err != nil {
		httpx.Error(w, err)
		return
	}
	key := r.Header.Get("Idempotency-Key")
	if err := domain.ValidateKey(key); err != nil {
		httpx.Error(w, err)
		return
	}
	if strings.TrimSpace(in.Prompt) == "" || len(in.Prompt) > 2000 {
		httpx.Error(w, domain.Invalid("prompt must contain 1–2000 characters"))
		return
	}
	if in.Scenario == "" {
		in.Scenario = "success"
	}
	switch in.Scenario {
	case "success", "decline", "unavailable", "delayed", "lost-response":
	default:
		httpx.Error(w, domain.Invalid("invalid scenario"))
		return
	}
	if agent.Stage < 5 && in.Scenario != "success" {
		httpx.Error(w, domain.Invalid("failure experiments start at workshop/05-recovery"))
		return
	}
	if in.ApprovalSeconds != 0 && (in.ApprovalSeconds < 1 || in.ApprovalSeconds > 600) {
		httpx.Error(w, domain.Invalid("approval_seconds must be 1–600"))
		return
	}
	id := s.Config.Prefix + "-" + domain.Fingerprint(key)[:24]
	hash := domain.Fingerprint(in)
	model := s.Config.Model
	if s.Config.Mode == "scripted" {
		model = "scripted"
	}
	_, err := s.Temporal.ExecuteWorkflow(r.Context(), client.StartWorkflowOptions{ID: id, TaskQueue: s.Config.Queue, WorkflowIDReusePolicy: enums.WORKFLOW_ID_REUSE_POLICY_REJECT_DUPLICATE, Memo: map[string]any{"request_fingerprint": hash}}, agent.AgentWorkflow, agent.Input{Prompt: in.Prompt, Model: model, Mode: s.Config.Mode, Scenario: in.Scenario, ApprovalSeconds: in.ApprovalSeconds})
	var exists *serviceerror.WorkflowExecutionAlreadyStarted
	if errors.As(err, &exists) {
		desc, e := s.Temporal.DescribeWorkflowExecution(r.Context(), id, "")
		if e != nil {
			httpx.Error(w, e)
			return
		}
		var old string
		if desc.WorkflowExecutionInfo.Memo == nil {
			httpx.Error(w, domain.Conflict("request key already used"))
			return
		}
		payload := desc.WorkflowExecutionInfo.Memo.Fields["request_fingerprint"]
		if e = converter.GetDefaultDataConverter().FromPayload(payload, &old); e != nil || old != hash {
			httpx.Error(w, domain.Conflict("request key belongs to a different prompt or scenario"))
			return
		}
	} else if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, 202, map[string]any{"request_id": id, "workflow_id": id, "model_mode": s.Config.Mode})
}
func (s *Server) validID(id string) bool { return strings.HasPrefix(id, s.Config.Prefix+"-") }
func (s *Server) state(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !s.validID(id) {
		httpx.Error(w, domain.NotFound("request not found in this environment"))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	desc, err := s.Temporal.DescribeWorkflowExecution(ctx, id, "")
	if err != nil {
		httpx.Error(w, err)
		return
	}
	if desc.WorkflowExecutionInfo.Status != enums.WORKFLOW_EXECUTION_STATUS_RUNNING {
		var state agent.State
		if err = s.Temporal.GetWorkflow(ctx, id, "").Get(ctx, &state); err != nil {
			httpx.Error(w, err)
			return
		}
		httpx.JSON(w, 200, state)
		return
	}
	value, err := s.Temporal.QueryWorkflow(ctx, id, "", "state")
	if err != nil {
		httpx.Error(w, err)
		return
	}
	var state agent.State
	if err = value.Get(&state); err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, 200, state)
}
func (s *Server) decision(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !s.validID(id) {
		httpx.Error(w, domain.NotFound("request not found"))
		return
	}
	var wire struct {
		ProposalID string `json:"proposal_id"`
		Approve    *bool  `json:"approve"`
	}
	if err := httpx.Decode(w, r, &wire); err != nil {
		httpx.Error(w, err)
		return
	}
	if wire.ProposalID == "" || wire.Approve == nil {
		httpx.Error(w, domain.Invalid("proposal_id and explicit boolean approve are required"))
		return
	}
	d := agent.Decision{ProposalID: wire.ProposalID, Approve: *wire.Approve}
	updateID := r.Header.Get("Idempotency-Key")
	if err := domain.ValidateKey(updateID); err != nil {
		httpx.Error(w, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	h, err := s.Temporal.UpdateWorkflow(ctx, client.UpdateWorkflowOptions{WorkflowID: id, UpdateID: updateID, UpdateName: "decision", Args: []any{d}, WaitForStage: client.WorkflowUpdateStageCompleted})
	if err != nil {
		httpx.Error(w, err)
		return
	}
	var out string
	if err = h.Get(ctx, &out); err != nil {
		httpx.Error(w, domain.Conflict(fmt.Sprintf("decision rejected: %v", err)))
		return
	}
	httpx.JSON(w, 200, map[string]string{"decision": out})
}
func (s *Server) transaction(w http.ResponseWriter, r *http.Request) {
	var out domain.Transfer
	err := s.Payment.Do(r.Context(), "GET", "/transfers/"+url.PathEscape(r.PathValue("reference")), "", nil, &out)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, 200, out)
}
