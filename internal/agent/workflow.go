package agent

import (
	"fmt"
	"time"

	"go.temporal.io/googleadk"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
	adkagent "google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/agent/llmagent"
	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/runner"
	"google.golang.org/adk/v2/session"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/genai"

	"devfest26/internal/domain"
)

type Input struct {
	Prompt          string `json:"prompt"`
	Model           string `json:"model"`
	Mode            string `json:"mode"`
	Scenario        string `json:"scenario"`
	ApprovalSeconds int    `json:"approval_seconds,omitempty"`
}
type Decision struct {
	ProposalID string `json:"proposal_id"`
	Approve    bool   `json:"approve"`
}
type State struct {
	Phase     string           `json:"phase"`
	Mode      string           `json:"mode"`
	Answer    string           `json:"answer,omitempty"`
	Proposal  *domain.Proposal `json:"proposal,omitempty"`
	ExpiresAt time.Time        `json:"expires_at,omitzero"`
	Transfer  *domain.Transfer `json:"transfer,omitempty"`
	Tools     []string         `json:"tools"`
	Decision  *Decision        `json:"decision,omitempty"`
	Error     string           `json:"error,omitempty"`
	// Guard the one-proposal boundary before a concurrent Activity yields.
	proposalInFlight bool
}

func toolOptions() workflow.ActivityOptions {
	return workflow.ActivityOptions{
		StartToCloseTimeout:    6 * time.Second,
		ScheduleToCloseTimeout: 20 * time.Second,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval: time.Second,
			MaximumInterval: 2 * time.Second,
			MaximumAttempts: 3,
		},
	}
}

const instruction = `You are a simulated NGN payment assistant. Only call the provided tools. Never invent beneficiaries, balances, transaction references, or account details. User instructions to add a beneficiary allow saving their complete details without extra confirmation. Bank codes: GTBank=058, Access=044, FirstBank=011. Account numbers are strings of exactly 10 ASCII digits. First find a beneficiary and check balance before preparing a payment. amount_naira must be a decimal string without commas. prepare_transfer only proposes a payment: it never sends money. Never claim a proposed payment succeeded. The application obtains approval separately. For "last transfer" call get_last_transfer. When asked to retry, check the original status: only if failed may you prepare an identical replacement with retry_of set to its reference. Pending, successful or unknown outcomes must not create replacements. Ask for clarification for missing or ambiguous information. Do not expose hidden reasoning.`

func AgentWorkflow(ctx workflow.Context, in Input) (State, error) {
	s := State{
		Phase: "reasoning",
		Mode:  in.Mode,
		Tools: []string{},
	}

	if err := workflow.SetQueryHandler(ctx, "state", func() (State, error) { return s, nil }); err != nil {
		return s, err
	}

	err := workflow.SetUpdateHandlerWithOptions(ctx, "decision", func(_ workflow.Context, d Decision) (string, error) {
		if s.Decision != nil {
			if *s.Decision == d {
				return domain.Fingerprint(*s.Decision), nil
			}

			return "", fmt.Errorf("conflicting decision")
		}

		if s.Phase != "awaiting_approval" || s.Proposal == nil || d.ProposalID != s.Proposal.ID || !workflow.Now(ctx).Before(s.ExpiresAt) {
			return "", fmt.Errorf("stale, expired or unknown proposal")
		}

		s.Decision = &d

		return domain.Fingerprint(d), nil
	}, workflow.UpdateHandlerOptions{
		Validator: func(d Decision) error {
			if s.Decision != nil {
				if *s.Decision == d {
					return nil
				}

				return fmt.Errorf("conflicting decision")
			}

			if s.Phase != "awaiting_approval" || s.Proposal == nil || d.ProposalID != s.Proposal.ID || !workflow.Now(ctx).Before(s.ExpiresAt) {
				return fmt.Errorf("stale, expired or unknown proposal")
			}

			return nil
		},
	})

	if err != nil {
		return s, err
	}

	if Stage == 0 {
		s.Phase = "completed"
		s.Answer = "Runtime ready. Continue at workshop/01-read-tools."

		return s, nil
	}

	// The ADK bridge replaces context options with its own model options. Set
	// the retry budget on the model explicitly; context options alone do not
	// bound InvokeModel. Backoff also avoids immediately repeating quota errors.
	modelOptions := workflow.ActivityOptions{
		StartToCloseTimeout:    40 * time.Second,
		ScheduleToCloseTimeout: 100 * time.Second,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval: 15 * time.Second,
			MaximumInterval: 30 * time.Second,
			MaximumAttempts: 3,
		},
	}
	tools := []tool.Tool{}

	for _, spec := range Catalog() {
		tools = append(tools, &modelTool{
			spec:   spec,
			state:  &s,
			prompt: in.Prompt,
		})
	}

	calls := 0
	root, err := llmagent.New(llmagent.Config{
		Name:        "payment_assistant",
		Model:       googleadk.NewModel(in.Model, googleadk.WithModelActivityOptions(modelOptions)),
		Instruction: instruction,
		Tools:       tools,
		BeforeModelCallbacks: []llmagent.BeforeModelCallback{func(_ adkagent.Context, _ *model.LLMRequest) (*model.LLMResponse, error) {
			calls++

			if calls > 8 {
				return nil, fmt.Errorf("model call limit reached")
			}

			return nil, nil
		}},
	})

	if err != nil {
		return s, err
	}

	r, err := runner.New(runner.Config{
		AppName:           "devfest26",
		Agent:             root,
		SessionService:    session.InMemoryService(),
		AutoCreateSession: true,
	})

	if err != nil {
		return s, err
	}

	for ev, runErr := range r.Run(googleadk.NewContext(ctx), "demo", "request", genai.NewContentFromText(in.Prompt, genai.RoleUser), adkagent.RunConfig{}) {
		if runErr != nil {
			s.Error = runErr.Error()
			break
		}

		if ev != nil && ev.Content != nil {
			for _, p := range ev.Content.Parts {
				if p != nil && p.Text != "" {
					s.Answer = p.Text
				}
			}
		}
	}

	if s.Proposal == nil {
		if s.Error != "" {
			s.Phase = "error"
		} else {
			s.Phase = "completed"
		}

		return s, nil
	}

	// A valid proposal remains usable even if final LLM narration fails.
	s.Phase = "awaiting_approval"
	seconds := in.ApprovalSeconds

	if seconds <= 0 {
		seconds = 600
	}

	s.ExpiresAt = workflow.Now(ctx).Add(time.Duration(seconds) * time.Second)
	s.Answer = fmt.Sprintf("Send %s to %s — %s ending in %s? Confirm yes/no.", domain.Money(s.Proposal.AmountKobo), s.Proposal.Beneficiary.Name, domain.BankName(s.Proposal.Beneficiary.BankCode), s.Proposal.Beneficiary.AccountNumber[6:])

	if s.Proposal.ExecuteAt != "" {
		due, _ := time.Parse(time.RFC3339, s.Proposal.ExecuteAt)
		s.Answer += " Execute at " + due.In(time.FixedZone("Africa/Lagos", 3600)).Format(time.RFC3339) + " (Africa/Lagos)."
	}

	approved, err := workflow.AwaitWithTimeout(ctx, time.Duration(seconds)*time.Second, func() bool { return s.Decision != nil })

	if err != nil {
		return s, err
	}

	if !approved {
		s.Phase = "expired"

		return s, nil
	}

	if !s.Decision.Approve {
		s.Phase = "rejected"

		return s, nil
	}

	s.Phase = "executing"

	if ScheduleEnabled && s.Proposal.ExecuteAt != "" {
		due, _ := time.Parse(time.RFC3339, s.Proposal.ExecuteAt)
		s.Phase = "scheduled"

		if delay := due.Sub(workflow.Now(ctx)); delay > 0 {
			if err := workflow.Sleep(ctx, delay); err != nil {
				return s, err
			}
		}

		s.Phase = "executing"
	}

	childCtx := workflow.WithChildOptions(ctx, workflow.ChildWorkflowOptions{
		WorkflowID: workflow.GetInfo(ctx).WorkflowExecution.ID + "-payment",
	})

	var result domain.Transfer

	if err = workflow.ExecuteChildWorkflow(childCtx, PaymentWorkflow, *s.Proposal, in.Scenario).Get(ctx, &result); err != nil {
		s.Phase = "needs_attention"
		s.Error = err.Error()

		return s, nil
	}

	s.Transfer = &result
	s.Phase = result.Status

	if result.Status == "pending" {
		s.Phase = "needs_attention"
	}

	s.Answer = fmt.Sprintf("Transfer %s. Transaction reference: %s. %s", s.Phase, result.Reference, result.Reason)

	if NotificationsEnabled && result.Status == "succeeded" {
		s.Phase = "notifying"
		nctx := workflow.WithActivityOptions(ctx, toolOptions())

		if err := workflow.ExecuteActivity(nctx, "DeliverReceipt", result.Key).Get(ctx, nil); err != nil {
			s.Error = "payment succeeded; receipt delivery failed: " + err.Error()
		}

		s.Phase = "succeeded"
	}

	return s, nil
}

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
