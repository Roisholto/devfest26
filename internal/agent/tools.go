package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"

	"go.temporal.io/googleadk"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/converter"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
	adkagent "google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/tool/toolutils"
	"google.golang.org/genai"

	"devfest26/internal/domain"
	"devfest26/internal/httpx"
)

type Spec struct {
	Name, Description string
	Fields            []string
	Required          []string
}

// Catalog is ordered: tool declaration order must remain stable during replay.
func Catalog() []Spec {
	s := []Spec{
		{"list_beneficiaries", "List saved beneficiaries", nil, nil},
		{"search_beneficiaries", "Search beneficiaries by part of a name", []string{"query"}, []string{"query"}},
		{"find_beneficiary", "Resolve exactly one saved beneficiary by name", []string{"name"}, []string{"name"}},
		{"check_balance", "Read available balance", nil, nil},
	}
	if Stage >= 3 {
		s = append(s, Spec{"add_beneficiary", "Save beneficiary details supplied in the user's add instruction. No extra confirmation required.", []string{"name", "bank_code", "account_number"}, []string{"name", "bank_code", "account_number"}})
	}
	if Stage == 2 {
		s = append(s, Spec{"add_beneficiary", "EXERCISE: save a beneficiary supplied by the user", []string{"name", "bank_code", "account_number"}, []string{"name", "bank_code", "account_number"}})
	}
	if Stage >= 4 {
		fields := []string{"beneficiary_id", "amount_naira", "retry_of"}
		if ScheduleEnabled {
			fields = append(fields, "execute_at")
		}
		s = append(s, Spec{"prepare_transfer", "Prepare one proposal. amount_naira is an exact decimal string without commas. retry_of is the reference of a definitively failed payment.", fields, []string{"beneficiary_id", "amount_naira"}}, Spec{"get_transaction_status", "Read the authoritative status by reference or payment key", []string{"reference"}, []string{"reference"}}, Spec{"get_last_transfer", "Retrieve the most recently created transfer", nil, nil})
	}
	return s
}

type PrepareArgs struct {
	BeneficiaryID string `json:"beneficiary_id"`
	Amount        string `json:"amount_naira"`
	RetryOf       string `json:"retry_of,omitempty"`
	ExecuteAt     string `json:"execute_at,omitempty"`
}
type Router struct {
	Client *httpx.Client
	Limit  int64
}

func toMap(v any) map[string]any {
	b, _ := json.Marshal(v)
	var out map[string]any
	_ = json.Unmarshal(b, &out)
	return out
}
func decode(args converter.EncodedValues, out any) error {
	var raw json.RawMessage
	if err := args.Get(&raw); err != nil {
		return domain.Invalid("malformed tool arguments")
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(out); err != nil {
		return domain.Invalid("invalid tool arguments: " + err.Error())
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return domain.Invalid("one argument object is required")
	}
	return nil
}
func activityError(err error) error {
	if err == nil {
		return nil
	}
	var p *domain.Problem
	if errors.As(err, &p) && p.Code != "unavailable" {
		return temporal.NewNonRetryableApplicationError(p.Message, p.Code, err)
	}
	return temporal.NewApplicationErrorWithCause("external dependency unavailable", "unavailable", err)
}

// Execute is the ONLY registration for model tools. Named static activities do
// not demonstrate Dynamic Activities; this native fallback does.
func (r *Router) Execute(ctx context.Context, args converter.EncodedValues) (any, error) {
	name := activity.GetInfo(ctx).ActivityType.Name
	var selected *Spec
	for _, s := range Catalog() {
		if s.Name == name {
			selected = &s
			break
		}
	}
	if selected == nil {
		return nil, activityError(domain.Invalid("unknown tool: " + name))
	}
	var fields map[string]json.RawMessage
	if err := args.Get(&fields); err != nil || fields == nil {
		return nil, activityError(domain.Invalid("tool arguments must be an object"))
	}
	for key, raw := range fields {
		known := false
		for _, field := range selected.Fields {
			if key == field {
				known = true
				break
			}
		}
		if !known {
			return nil, activityError(domain.Invalid("unknown field: " + key))
		}
		var value string
		if string(raw) == "null" || json.Unmarshal(raw, &value) != nil {
			return nil, activityError(domain.Invalid(key + " must be a string"))
		}
	}
	for _, key := range selected.Required {
		if _, ok := fields[key]; !ok {
			return nil, activityError(domain.Invalid("missing field: " + key))
		}
	}
	result, err := r.dispatch(ctx, name, args)
	return result, activityError(err)
}
func (r *Router) dispatch(ctx context.Context, name string, args converter.EncodedValues) (any, error) {
	info := activity.GetInfo(ctx)
	key := info.WorkflowExecution.ID + "/" + info.ActivityID
	switch name {
	case "list_beneficiaries", "search_beneficiaries", "find_beneficiary":
		var in struct {
			Query string `json:"query,omitempty"`
			Name  string `json:"name,omitempty"`
		}
		if name == "list_beneficiaries" {
			var empty struct{}
			if err := decode(args, &empty); err != nil {
				return nil, err
			}
		} else if err := decode(args, &in); err != nil {
			return nil, err
		}
		q := in.Query
		if name == "find_beneficiary" {
			q = in.Name
		}
		if name != "list_beneficiaries" && (strings.TrimSpace(q) == "" || len(q) > 80) {
			return nil, domain.Invalid("a search/name of 1–80 characters is required")
		}
		var bs []domain.Beneficiary
		if err := r.Client.Do(ctx, "GET", "/beneficiaries?q="+url.QueryEscape(q), "", nil, &bs); err != nil {
			return nil, err
		}
		if name == "find_beneficiary" {
			if len(bs) != 1 {
				return nil, domain.Invalid(fmt.Sprintf("found %d matching beneficiaries; ask for clarification", len(bs)))
			}
			return toMap(bs[0]), nil
		}
		return map[string]any{"beneficiaries": bs}, nil
	case "add_beneficiary":
		var in domain.AddBeneficiary
		if err := decode(args, &in); err != nil {
			return nil, err
		}
		return r.AddBeneficiary(ctx, key, in)
	case "check_balance":
		var empty struct{}
		if err := decode(args, &empty); err != nil {
			return nil, err
		}
		var out map[string]any
		err := r.Client.Do(ctx, "GET", "/balance", "", nil, &out)
		return out, err
	case "get_transaction_status", "get_last_transfer":
		path := "/transfers/last"
		if name == "get_transaction_status" {
			var in struct {
				Reference string `json:"reference"`
			}
			if err := decode(args, &in); err != nil {
				return nil, err
			}
			if in.Reference == "" {
				return nil, domain.Invalid("reference is required")
			}
			path = "/transfers/" + url.PathEscape(in.Reference)
		} else {
			var empty struct{}
			if err := decode(args, &empty); err != nil {
				return nil, err
			}
		}
		var out domain.Transfer
		err := r.Client.Do(ctx, "GET", path, "", nil, &out)
		return toMap(out), err
	case "prepare_transfer":
		var in PrepareArgs
		if err := decode(args, &in); err != nil {
			return nil, err
		}
		n, err := domain.ParseAmount(in.Amount, r.Limit)
		if err != nil {
			return nil, err
		}
		if in.ExecuteAt != "" {
			if !ScheduleEnabled {
				return nil, domain.Invalid("scheduled transfers are a follow-up lab")
			}
			due, err := time.Parse(time.RFC3339, in.ExecuteAt)
			if err != nil || !due.After(time.Now()) {
				return nil, domain.Invalid("execute_at must be a future RFC3339 timestamp with timezone")
			}
		}
		var bs []domain.Beneficiary
		if err = r.Client.Do(ctx, "GET", "/beneficiaries", "", nil, &bs); err != nil {
			return nil, err
		}
		var b domain.Beneficiary
		for _, v := range bs {
			if v.ID == in.BeneficiaryID {
				b = v
				break
			}
		}
		if b.ID == "" {
			return nil, domain.Invalid("unknown beneficiary_id")
		}
		if in.RetryOf != "" {
			var old domain.Transfer
			if err = r.Client.Do(ctx, "GET", "/transfers/"+url.PathEscape(in.RetryOf), "", nil, &old); err != nil {
				return nil, err
			}
			if old.Status != "failed" || old.AmountKobo != n || old.BeneficiaryID != b.ID {
				return nil, domain.Invalid("replacement must match a definitively failed transfer")
			}
		}
		var balance struct {
			Balance int64 `json:"balance_kobo"`
		}
		if err = r.Client.Do(ctx, "GET", "/balance", "", nil, &balance); err != nil {
			return nil, err
		}
		if balance.Balance < n {
			return nil, domain.Invalid("insufficient funds for this proposal")
		}
		return toMap(domain.Proposal{ID: domain.Fingerprint(key)[:16], Beneficiary: b, AmountKobo: n, Currency: "NGN", PaymentKey: info.WorkflowExecution.ID + "/payment", ExecuteAt: in.ExecuteAt}), nil
	}
	return nil, domain.Invalid("unknown tool")
}

// modelTool is a workflow-side ADK adapter. Its only I/O is a named Activity.
type modelTool struct {
	spec   Spec
	state  *State
	prompt string
}

func (t *modelTool) Name() string        { return t.spec.Name }
func (t *modelTool) Description() string { return t.spec.Description }
func (t *modelTool) IsLongRunning() bool { return false }
func (t *modelTool) Declaration() *genai.FunctionDeclaration {
	p := map[string]*genai.Schema{}
	for _, f := range t.spec.Fields {
		p[f] = &genai.Schema{Type: genai.TypeString}
	}
	return &genai.FunctionDeclaration{Name: t.Name(), Description: t.Description(), Parameters: &genai.Schema{Type: genai.TypeObject, Properties: p, Required: t.spec.Required}}
}
func (t *modelTool) ProcessRequest(_ adkagent.Context, req *model.LLMRequest) error {
	return toolutils.PackTool(req, t)
}
func (t *modelTool) Run(toolCtx adkagent.Context, args any) (map[string]any, error) {
	// ADK omits the empty Args map on a zero-argument function call. Normalize
	// that SDK representation before crossing the strict Activity boundary.
	if len(t.spec.Fields) == 0 && toMap(args) == nil {
		args = map[string]any{}
	}
	if t.state.Error != "" {
		return nil, fmt.Errorf("a previous tool failed; ask the user for a corrected request")
	}
	if t.Name() == "prepare_transfer" && (t.state.Proposal != nil || t.state.proposalInFlight) {
		return nil, domain.Invalid("only one proposal is allowed per request")
	}
	if t.Name() == "prepare_transfer" && strings.Contains(strings.ToLower(t.prompt), "retry") {
		m := toMap(args)
		if m["retry_of"] == nil || m["retry_of"] == "" {
			return nil, domain.Invalid("retry requires a definitively failed transaction reference")
		}
	}
	// ADK can fan out several tool calls in one turn. Each Temporal coroutine
	// must block on its own context, never the enclosing workflow's context.
	active, ok := googleadk.WorkflowContext(toolCtx)
	if !ok {
		return nil, fmt.Errorf("tool requires a Temporal ADK context")
	}
	if t.Name() == "prepare_transfer" {
		t.state.proposalInFlight = true
		defer func() { t.state.proposalInFlight = false }()
	}
	ctx := workflow.WithActivityOptions(active, toolOptions())
	var out map[string]any
	err := workflow.ExecuteActivity(ctx, t.Name(), args).Get(ctx, &out)
	if err != nil {
		t.state.Error = err.Error()
		return nil, err
	}
	t.state.Tools = append(t.state.Tools, t.Name())
	if t.Name() == "prepare_transfer" {
		b, _ := json.Marshal(out)
		var p domain.Proposal
		if err := json.Unmarshal(b, &p); err != nil {
			return nil, err
		}
		t.state.Proposal = &p
	}
	return out, nil
}
