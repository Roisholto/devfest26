package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"iter"
	"os"
	"sync"

	"go.temporal.io/sdk/activity"
	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/model/gemini"
	"google.golang.org/genai"
)

// NewGemini lets Temporal own retries rather than multiplying them in the SDK.
func NewGemini(ctx context.Context, name string) (model.LLM, error) {
	key := os.Getenv("GEMINI_API_KEY")

	if key == "" {
		return nil, fmt.Errorf("GEMINI_API_KEY is required")
	}

	attempts := int32(1)

	return gemini.NewModel(ctx, name, &genai.ClientConfig{
		APIKey:  key,
		Backend: genai.BackendGeminiAPI,
		HTTPOptions: genai.HTTPOptions{
			RetryOptions: &genai.HTTPRetryOptions{
				Attempts: &attempts,
			},
		},
	})
}

var auditMu sync.Mutex

// AuditedModel decorates an ADK model with JSONL invocation events for each
// Temporal Activity attempt. It requires an Activity context and records
// Workflow, Activity, attempt and model identifiers for recovery verification.
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
