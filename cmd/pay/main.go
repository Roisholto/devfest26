package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"devfest26/internal/agent"
	"devfest26/internal/config"
	"devfest26/internal/domain"
	"devfest26/internal/httpx"
)

// workflowResponse is the CLI's JSON view of an agent request. It combines the
// last observed WorkflowState with request/decision keys and resume metadata
// so a caller can continue the same request after a bounded wait expires.
type workflowResponse struct {
	*agent.WorkflowState
	RequestID      string `json:"request_id,omitempty"`
	WorkflowID     string `json:"workflow_id,omitempty"`
	RequestKey     string `json:"request_key,omitempty"`
	DecisionKey    string `json:"decision_key,omitempty"`
	DecisionStatus string `json:"decision_status,omitempty"`
	WaitTimedOut   bool   `json:"wait_timed_out,omitempty"`
	ResumeCommand  string `json:"resume_command,omitempty"`
}

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)

	defer cancel()

	if err := run(ctx, os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func printJSON(w io.Writer, v any) error {
	e := json.NewEncoder(w)
	e.SetIndent("", "  ")

	return e.Encode(v)
}

func run(ctx context.Context, args []string, w io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: pay ask|get|wait|approve|reject|transaction|reconcile|evidence [flags] [argument]")
	}

	cmd := args[0]
	f := flag.NewFlagSet(cmd, flag.ContinueOnError)
	key := f.String("key", "", "stable request or decision key (returned in the response)")
	scenario := f.String("scenario", "success", "success|decline|unavailable|delayed|lost-response")
	terminal := f.Bool("terminal", false, "wait through approval/scheduling to a terminal outcome")
	timeout := f.Duration("timeout", 150*time.Second, "maximum time for the command, including waiting for its response")
	expiry := f.Int("approval-seconds", 600, "approval expiry, 1–600 seconds")

	if err := f.Parse(args[1:]); err != nil {
		return err
	}

	if *timeout <= 0 {
		return fmt.Errorf("timeout must be positive")
	}

	arg := strings.Join(f.Args(), " ")

	if err := config.LoadDotEnv(".env"); err != nil {
		return err
	}

	cfg := config.Load()
	c := httpx.NewClient("http://" + cfg.API)
	// Allow the API's five-second queries and eight-second decision Updates.
	// The command context still bounds every call and the entire wait.
	c.HTTP.Timeout = 10 * time.Second

	ctx, cancel := context.WithTimeout(ctx, *timeout)

	defer cancel()

	if cmd == "evidence" {
		var out any

		if err := httpx.NewClient("http://"+cfg.Payment).Do(ctx, "GET", "/evidence", "", nil, &out); err != nil {
			return err
		}

		return printJSON(w, out)
	}

	if arg == "" {
		return fmt.Errorf("%s requires a prompt, request ID or transaction reference", cmd)
	}

	switch cmd {
	case "ask":
		if *key == "" {
			*key = domain.NewID()
		}

		out := workflowResponse{
			RequestKey: *key,
		}

		err := c.Do(ctx, "POST", "/requests", *key, map[string]any{"prompt": arg, "scenario": *scenario, "approval_seconds": *expiry}, &out)

		if err != nil {
			var problem *domain.Problem

			if errors.As(err, &problem) && problem.Code != "unavailable" {
				return respond(w, out, err)
			}

			return respond(w, out, fmt.Errorf("submission response unavailable; retry the same prompt and flags with --key %s: %w", *key, err))
		}

		if out.RequestID == "" {
			return respond(w, out, fmt.Errorf("API accepted the request without a request_id; retain the request key"))
		}

		// The start acknowledgement is metadata, not a Workflow state snapshot.
		out.WorkflowState = nil

		return awaitResponse(ctx, c, w, out, *terminal)
	case "get":
		var out agent.WorkflowState

		if err := c.Do(ctx, "GET", "/requests/"+url.PathEscape(arg), "", nil, &out); err != nil {
			return err
		}

		return printJSON(w, out)
	case "wait":
		return awaitResponse(ctx, c, w, workflowResponse{
			RequestID:  arg,
			WorkflowID: arg,
		}, *terminal)
	case "approve", "reject":
		out := workflowResponse{
			RequestID:      arg,
			WorkflowID:     arg,
			DecisionStatus: "not_submitted",
		}

		var state agent.WorkflowState

		if err := c.Do(ctx, "GET", "/requests/"+url.PathEscape(arg), "", nil, &state); err != nil {
			return respond(w, out, fmt.Errorf("decision not submitted; could not read the proposal: %w", err))
		}

		out.WorkflowState = &state

		if state.Proposal == nil {
			return respond(w, out, fmt.Errorf("request has no proposal"))
		}

		d := agent.ApprovalDecision{
			ProposalID: state.Proposal.ID,
			Approve:    cmd == "approve",
		}

		if *key == "" {
			*key = "decision-" + domain.Fingerprint(d)[:24]
		}

		out.DecisionKey = *key
		out.DecisionStatus = "unconfirmed"
		out.ResumeCommand = "go run ./cmd/pay wait " + arg

		if err := c.Do(ctx, "POST", "/requests/"+url.PathEscape(arg)+"/decision", *key, d, nil); err != nil {
			var problem *domain.Problem

			if errors.As(err, &problem) && problem.Code != "unavailable" {
				out.DecisionStatus = "not_accepted"

				return respond(w, out, err)
			}

			return respond(w, out, fmt.Errorf("decision not confirmed; inspect the request before retrying with the same decision key: %w", err))
		}

		out.DecisionStatus = "accepted"
		out.ResumeCommand = ""
		out.WorkflowState = nil

		return awaitResponse(ctx, c, w, out, true)
	case "transaction", "reconcile":
		method, path := "GET", "/transactions/"+url.PathEscape(arg)

		if cmd == "reconcile" {
			method = "POST"
			path += "/reconcile"
		}

		var out any

		if err := c.Do(ctx, method, path, "", nil, &out); err != nil {
			return err
		}

		return printJSON(w, out)
	}

	return fmt.Errorf("unknown command %q", cmd)
}

// respond preserves request identity even when an acknowledgement is lost.
func respond(w io.Writer, out workflowResponse, err error) error {
	if writeErr := printJSON(w, out); writeErr != nil {
		return writeErr
	}

	return err
}

func awaitResponse(ctx context.Context, c *httpx.Client, w io.Writer, out workflowResponse, terminal bool) error {
	state, err := waitForState(ctx, c, out.RequestID, terminal)

	if state != nil {
		out.WorkflowState = state
	}

	if err != nil {
		out.WaitTimedOut = errors.Is(err, context.DeadlineExceeded)
		out.ResumeCommand = "go run ./cmd/pay wait "

		if terminal {
			out.ResumeCommand += "--terminal "
		}

		out.ResumeCommand += out.RequestID
	}

	// Reaching a local deadline is an expected pending response, not a failed
	// workflow. No cancellation is sent to Temporal.
	if out.WaitTimedOut {
		err = nil
	}

	return respond(w, out, err)
}

func waitForState(ctx context.Context, c *httpx.Client, id string, terminal bool) (*agent.WorkflowState, error) {
	var last *agent.WorkflowState

	for {
		var state agent.WorkflowState

		err := c.Do(ctx, "GET", "/requests/"+url.PathEscape(id), "", nil, &state)

		if err == nil {
			last = &state
			waiting := state.Phase == "reasoning" || state.Phase == "executing" || state.Phase == "notifying" || (terminal && (state.Phase == "awaiting_approval" || state.Phase == "scheduled"))

			if !waiting {
				return last, nil
			}
		} else {
			if ctx.Err() != nil {
				return last, ctx.Err()
			}

			var problem *domain.Problem
			var networkError net.Error

			if !(errors.As(err, &problem) && problem.Code == "unavailable") && !errors.As(err, &networkError) {
				return last, err
			}
		}

		timer := time.NewTimer(time.Second)

		select {
		case <-ctx.Done():
			timer.Stop()

			return last, ctx.Err()
		case <-timer.C:
		}
	}
}
