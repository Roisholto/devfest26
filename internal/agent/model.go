package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"iter"
	"os"
	"regexp"
	"strings"
	"sync"

	"go.temporal.io/googleadk"
	"go.temporal.io/sdk/activity"
	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"
)

// ScriptedModel is a labelled offline demonstration, not a general language
// model. It runs behind the same InvokeModel Activity and ADK tool loop.
type ScriptedModel struct{}

func (ScriptedModel) Name() string { return "scripted" }

var addRE = regexp.MustCompile(`(?i)^add\s+(.+?)\s+at\s+(GTBank|Access|FirstBank),?\s+account\s+(\S+?)\.?$`)
var sendRE = regexp.MustCompile(`(?i)^send\s+₦?([0-9,]+(?:\.[0-9]{1,2})?)\s+to\s+(.+?)\.?$`)
var searchRE = regexp.MustCompile(`(?i)^search\s+(?:beneficiaries\s+)?(?:for\s+)?(.+?)\.?$`)
var scheduleRE = regexp.MustCompile(`(?i)^schedule\s+₦?([0-9,]+(?:\.[0-9]{1,2})?)\s+to\s+(.+?)\s+at\s+(\S+)$`)

func (ScriptedModel) GenerateContent(_ context.Context, req *model.LLMRequest, _ bool) iter.Seq2[*model.LLMResponse, error] {
	return func(yield func(*model.LLMResponse, error) bool) {
		prompt := ""
		results := map[string]map[string]any{}

		for _, c := range req.Contents {
			if c == nil {
				continue
			}

			for _, p := range c.Parts {
				if p == nil {
					continue
				}

				if c.Role == genai.RoleUser && p.Text != "" {
					prompt = p.Text
				}

				if p.FunctionResponse != nil {
					results[p.FunctionResponse.Name] = p.FunctionResponse.Response
				}
			}
		}

		text := func(s string) { yield(googleadk.TextResponse("[SCRIPTED] "+s), nil) }
		call := func(name string, args map[string]any) {
			yield(googleadk.FunctionCallResponse("call-"+fmt.Sprint(len(results)+1), name, args), nil)
		}
		p := strings.TrimSpace(prompt)
		low := strings.ToLower(p)

		for _, result := range results {
			if failure, ok := result["error"]; ok {
				text(fmt.Sprint("The operation failed: ", failure, ". Submit a corrected request."))

				return
			}
		}

		if m := addRE.FindStringSubmatch(p); m != nil {
			if out := results["add_beneficiary"]; out != nil {
				b, _ := json.Marshal(out)
				text("Beneficiary saved: " + string(b))

				return
			}

			bank := map[string]string{"gtbank": "058", "access": "044", "firstbank": "011"}[strings.ToLower(m[2])]
			call("add_beneficiary", map[string]any{"name": m[1], "bank_code": bank, "account_number": m[3]})

			return
		}

		if strings.Contains(low, "list") && strings.Contains(low, "beneficiar") {
			if out := results["list_beneficiaries"]; out != nil {
				b, _ := json.Marshal(out)
				text(string(b))

				return
			}

			call("list_beneficiaries", map[string]any{})

			return
		}

		if m := searchRE.FindStringSubmatch(p); m != nil {
			if out := results["search_beneficiaries"]; out != nil {
				b, _ := json.Marshal(out)
				text(string(b))

				return
			}

			call("search_beneficiaries", map[string]any{"query": m[1]})

			return
		}

		if strings.Contains(low, "last transfer") || strings.Contains(low, "retry") {
			out := results["get_last_transfer"]

			if out == nil {
				call("get_last_transfer", map[string]any{})

				return
			}

			status := results["get_transaction_status"]

			if status == nil {
				call("get_transaction_status", map[string]any{"reference": out["reference"]})

				return
			}

			out = status

			if !strings.Contains(low, "retry") || out["status"] != "failed" {
				b, _ := json.Marshal(out)
				text(string(b))

				return
			}

			if results["prepare_transfer"] != nil {
				text("Replacement proposed. Application confirmation is required.")

				return
			}

			amount, _ := out["amount_kobo"].(float64)
			call("prepare_transfer", map[string]any{"beneficiary_id": out["beneficiary_id"], "amount_naira": fmt.Sprintf("%.2f", amount/100), "retry_of": out["reference"]})

			return
		}

		m := sendRE.FindStringSubmatch(p)
		due := ""

		if scheduled := scheduleRE.FindStringSubmatch(p); scheduled != nil && ScheduleEnabled {
			m = scheduled
			due = scheduled[3]
		}

		if m != nil {
			b := results["find_beneficiary"]

			if b == nil {
				call("find_beneficiary", map[string]any{"name": m[2]})

				return
			}

			if results["check_balance"] == nil {
				call("check_balance", map[string]any{})

				return
			}

			if results["prepare_transfer"] == nil {
				args := map[string]any{"beneficiary_id": b["id"], "amount_naira": strings.ReplaceAll(m[1], ",", "")}

				if due != "" {
					args["execute_at"] = due
				}

				call("prepare_transfer", args)

				return
			}

			text("Proposal prepared. Application confirmation is required; no money has moved.")

			return
		}

		if strings.Contains(low, "balance") {
			if out := results["check_balance"]; out != nil {
				b, _ := json.Marshal(out)
				text(string(b))

				return
			}

			call("check_balance", map[string]any{})

			return
		}

		text("Unsupported offline prompt. Use: List my beneficiaries; Search beneficiaries for Ayo; Add Ayomide at GTBank, account 0123454821; Send ₦10,000 to Ayomide; Check my last transfer and retry it if it failed.")
	}
}

var auditMu sync.Mutex

type AuditedModel struct {
	Inner model.LLM
	Path  string
}

func (m AuditedModel) Name() string { return m.Inner.Name() }

func (m AuditedModel) record(ctx context.Context, event string) error {
	i := activity.GetInfo(ctx)
	b, _ := json.Marshal(map[string]any{"event": event, "workflow_id": i.WorkflowExecution.ID, "run_id": i.WorkflowExecution.RunID, "activity_id": i.ActivityID, "attempt": i.Attempt, "model": m.Name()})
	auditMu.Lock()

	defer auditMu.Unlock()

	f, err := os.OpenFile(m.Path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)

	if err != nil {
		return err
	}

	defer f.Close()

	_, err = f.Write(append(b, '\n'))

	return err
}

func (m AuditedModel) GenerateContent(ctx context.Context, req *model.LLMRequest, stream bool) iter.Seq2[*model.LLMResponse, error] {
	return func(yield func(*model.LLMResponse, error) bool) {
		if err := m.record(ctx, "model_invocation_started"); err != nil {
			yield(nil, err)

			return
		}

		for res, err := range m.Inner.GenerateContent(ctx, req, stream) {
			if err != nil {
				_ = m.record(ctx, "model_invocation_failed")
				yield(res, err)

				return
			}

			if !yield(res, nil) {
				return
			}
		}

		_ = m.record(ctx, "model_invocation_completed")
	}
}
