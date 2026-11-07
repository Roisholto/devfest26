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

// ToolSpec declares an allowed model-visible tool and its string-valued JSON
// arguments. The ordered catalog supplies both ADK declarations and the Dynamic
// Activity dispatcher's allowlist.
type ToolSpec struct {
	Name        string
	Description string

	// Fields lists the ordered JSON argument names; every value is a string.
	Fields []string

	// Required is the subset of Fields that must appear in a tool call.
	Required []string
}

// Catalog is ordered: tool declaration order must remain stable during replay.
func Catalog() []ToolSpec {
	return []ToolSpec{
		{
			Name:        "list_beneficiaries",
			Description: "List saved beneficiaries",
		},
		{
			Name:        "search_beneficiaries",
			Description: "Search beneficiaries by part of a name",
			Fields:      []string{"query"},
			Required:    []string{"query"},
		},
		{
			Name:        "find_beneficiary",
			Description: "Resolve exactly one saved beneficiary by name",
			Fields:      []string{"name"},
			Required:    []string{"name"},
		},
		{
			Name:        "check_balance",
			Description: "Read available balance",
		},
		{
			Name:        "prepare_transfer",
			Description: "Prepare one proposal. amount_naira is an exact decimal string without commas. retry_of is the reference of a definitively failed payment.",
			Fields:      []string{"beneficiary_id", "amount_naira", "retry_of"},
			Required:    []string{"beneficiary_id", "amount_naira"},
		},
		{
			Name:        "get_transaction_status",
			Description: "Read authoritative status by reference or payment key",
			Fields:      []string{"reference"},
			Required:    []string{"reference"},
		},
		{
			Name:        "get_last_transfer",
			Description: "Retrieve the most recently created transfer",
		},
		// EXERCISE 1: append the add_beneficiary ToolSpec here.
	}
}

// PrepareTransferArgs is the input to the prepare_transfer tool. Validation
// converts these arguments into a canonical [domain.TransferProposal] for explicit human
// approval; preparing a proposal leaves the account balance unchanged.
type PrepareTransferArgs struct {
	BeneficiaryID string `json:"beneficiary_id"`

	// AmountNaira is an exact decimal naira string, converted to integer kobo.
	AmountNaira string `json:"amount_naira"`

	// RetryOfReference identifies the definitively failed transfer being replaced.
	RetryOfReference string `json:"retry_of,omitempty"`
}

// ToolDispatcher implements the native Temporal Dynamic Activity handler for
// model tool calls. It validates catalog names and arguments before invoking
// the corresponding handler against the payment service.
type ToolDispatcher struct {
	// Client connects to the simulated payment service.
	Client *httpx.Client

	// Limit is the maximum permitted transfer amount, in integer kobo.
	Limit int64
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
func (r *ToolDispatcher) Execute(ctx context.Context, args converter.EncodedValues) (any, error) {
	name := activity.GetInfo(ctx).ActivityType.Name

	var selected *ToolSpec

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

// dispatch decodes tool-specific arguments after Execute validates the catalog
// schema. Zero-argument tools have already passed that common validation.
func (r *ToolDispatcher) dispatch(ctx context.Context, name string, args converter.EncodedValues) (any, error) {
	info := activity.GetInfo(ctx)
	key := info.WorkflowExecution.ID + "/" + info.ActivityID

	switch name {
	case "list_beneficiaries":
		return r.ListBeneficiaries(ctx)
	case "search_beneficiaries":
		var in struct {
			Query string `json:"query"`
		}

		if err := decode(args, &in); err != nil {
			return nil, err
		}

		return r.SearchBeneficiaries(ctx, in.Query)
	case "find_beneficiary":
		var in struct {
			Name string `json:"name"`
		}

		if err := decode(args, &in); err != nil {
			return nil, err
		}

		return r.FindBeneficiary(ctx, in.Name)
	case "add_beneficiary":
		var in domain.AddBeneficiaryRequest

		if err := decode(args, &in); err != nil {
			return nil, err
		}

		return r.AddBeneficiary(ctx, key, in)
	case "check_balance":
		return r.CheckBalance(ctx)
	case "get_transaction_status":
		var in struct {
			Reference string `json:"reference"`
		}

		if err := decode(args, &in); err != nil {
			return nil, err
		}

		return r.GetTransactionStatus(ctx, in.Reference)
	case "get_last_transfer":
		return r.GetLastTransfer(ctx)
	case "prepare_transfer":
		var in PrepareTransferArgs

		if err := decode(args, &in); err != nil {
			return nil, err
		}

		return r.PrepareTransfer(ctx, key, in)
	}

	return nil, domain.Invalid("unknown tool")
}

// ListBeneficiaries returns the saved recipients without changing provider state.
func (r *ToolDispatcher) ListBeneficiaries(ctx context.Context) (map[string]any, error) {
	beneficiaries, err := r.readBeneficiaries(ctx, "")

	if err != nil {
		return nil, err
	}

	return map[string]any{"beneficiaries": beneficiaries}, nil
}

// SearchBeneficiaries finds saved recipients whose names match the supplied text.
func (r *ToolDispatcher) SearchBeneficiaries(ctx context.Context, query string) (map[string]any, error) {
	if strings.TrimSpace(query) == "" || len(query) > 80 {
		return nil, domain.Invalid("a search/name of 1–80 characters is required")
	}

	beneficiaries, err := r.readBeneficiaries(ctx, query)

	if err != nil {
		return nil, err
	}

	return map[string]any{"beneficiaries": beneficiaries}, nil
}

// FindBeneficiary resolves exactly one recipient; ambiguous names require
// clarification before the agent can prepare a payment proposal.
func (r *ToolDispatcher) FindBeneficiary(ctx context.Context, name string) (map[string]any, error) {
	if strings.TrimSpace(name) == "" || len(name) > 80 {
		return nil, domain.Invalid("a search/name of 1–80 characters is required")
	}

	beneficiaries, err := r.readBeneficiaries(ctx, name)

	if err != nil {
		return nil, err
	}

	if len(beneficiaries) != 1 {
		return nil, domain.Invalid(fmt.Sprintf("found %d matching beneficiaries; ask for clarification", len(beneficiaries)))
	}

	return toMap(beneficiaries[0]), nil
}

func (r *ToolDispatcher) readBeneficiaries(ctx context.Context, query string) ([]domain.Beneficiary, error) {
	var beneficiaries []domain.Beneficiary

	err := r.Client.Do(ctx, "GET", "/beneficiaries?q="+url.QueryEscape(query), "", nil, &beneficiaries)

	return beneficiaries, err
}

// CheckBalance reads the provider's available balance and currency.
func (r *ToolDispatcher) CheckBalance(ctx context.Context) (map[string]any, error) {
	var out map[string]any

	err := r.Client.Do(ctx, "GET", "/balance", "", nil, &out)

	return out, err
}

// GetTransactionStatus reads the authoritative outcome for a reference or key.
func (r *ToolDispatcher) GetTransactionStatus(ctx context.Context, reference string) (map[string]any, error) {
	if reference == "" {
		return nil, domain.Invalid("reference is required")
	}

	return r.readTransfer(ctx, reference)
}

// GetLastTransfer retrieves the most recently created transfer from the provider.
func (r *ToolDispatcher) GetLastTransfer(ctx context.Context) (map[string]any, error) {
	return r.readTransfer(ctx, "last")
}

func (r *ToolDispatcher) readTransfer(ctx context.Context, reference string) (map[string]any, error) {
	var out domain.Transfer

	err := r.Client.Do(ctx, "GET", "/transfers/"+url.PathEscape(reference), "", nil, &out)

	return toMap(out), err
}

// PrepareTransfer validates the amount, recipient, replacement eligibility and
// current balance, then returns a proposal for human approval. It runs inside
// an Activity and uses that Activity's Workflow identity for the payment key.
func (r *ToolDispatcher) PrepareTransfer(ctx context.Context, key string, in PrepareTransferArgs) (map[string]any, error) {
	amountKobo, err := domain.ParseAmount(in.AmountNaira, r.Limit)

	if err != nil {
		return nil, err
	}

	if in.BeneficiaryID == "" {
		return nil, domain.Invalid("beneficiary_id is required")
	}

	var beneficiary domain.Beneficiary

	if err = r.Client.Do(ctx, "GET", "/beneficiaries/"+url.PathEscape(in.BeneficiaryID), "", nil, &beneficiary); err != nil {
		return nil, err
	}

	if in.RetryOfReference != "" {
		var original domain.Transfer

		if err = r.Client.Do(ctx, "GET", "/transfers/"+url.PathEscape(in.RetryOfReference), "", nil, &original); err != nil {
			return nil, err
		}

		if original.Status != "failed" || original.AmountKobo != amountKobo || original.BeneficiaryID != beneficiary.ID {
			return nil, domain.Invalid("replacement must match a definitively failed transfer")
		}
	}

	var balance struct {
		Balance int64 `json:"balance_kobo"`
	}

	if err = r.Client.Do(ctx, "GET", "/balance", "", nil, &balance); err != nil {
		return nil, err
	}

	if balance.Balance < amountKobo {
		return nil, domain.Invalid("insufficient funds for this proposal")
	}

	return toMap(domain.TransferProposal{
		ID:          domain.Fingerprint(key)[:16],
		Beneficiary: beneficiary,
		AmountKobo:  amountKobo,
		Currency:    "NGN",
		PaymentKey:  activity.GetInfo(ctx).WorkflowExecution.ID + "/payment",
	}), nil
}

// workflowTool adapts a ToolSpec to ADK inside AgentWorkflow. It schedules a
// named Dynamic Activity and incorporates its recorded result into WorkflowState.
type workflowTool struct {
	spec   ToolSpec
	state  *WorkflowState
	prompt string
}

func (t *workflowTool) Name() string { return t.spec.Name }

func (t *workflowTool) Description() string { return t.spec.Description }

func (t *workflowTool) IsLongRunning() bool { return false }

func (t *workflowTool) Declaration() *genai.FunctionDeclaration {
	p := map[string]*genai.Schema{}

	for _, f := range t.spec.Fields {
		p[f] = &genai.Schema{
			Type: genai.TypeString,
		}
	}

	return &genai.FunctionDeclaration{
		Name:        t.Name(),
		Description: t.Description(),
		Parameters: &genai.Schema{
			Type:       genai.TypeObject,
			Properties: p,
			Required:   t.spec.Required,
		},
	}
}

func (t *workflowTool) ProcessRequest(_ adkagent.Context, req *model.LLMRequest) error {
	return toolutils.PackTool(req, t)
}

func (t *workflowTool) Run(toolCtx adkagent.Context, args any) (map[string]any, error) {
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

		var p domain.TransferProposal

		if err := json.Unmarshal(b, &p); err != nil {
			return nil, err
		}

		t.state.Proposal = &p
	}

	return out, nil
}
