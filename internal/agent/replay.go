package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"go.temporal.io/api/enums/v1"
	historypb "go.temporal.io/api/history/v1"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"
	"go.temporal.io/sdk/workflow"
	"google.golang.org/protobuf/encoding/protojson"
)

// Replay fetches a real history and verifies that workflow code recreates the
// same commands without executing Activities or spending model tokens. The tool
// catalog must match the recorded run, including any completed workshop exercise.
func Replay(ctx context.Context, c client.Client, id, path string) error {
	it := c.GetWorkflowHistory(ctx, id, "", false, enums.HISTORY_EVENT_FILTER_TYPE_ALL_EVENT)
	h := &historypb.History{}

	for it.HasNext() {
		e, err := it.Next()

		if err != nil {
			return err
		}

		h.Events = append(h.Events, e)
	}

	if path != "" {
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			return err
		}

		b, err := (protojson.MarshalOptions{
			Indent: "  ",
		}).Marshal(h)

		if err != nil {
			return err
		}

		if err = os.WriteFile(path, b, 0600); err != nil {
			return err
		}
	}

	r := worker.NewWorkflowReplayer()
	r.RegisterWorkflow(AgentWorkflow)
	r.RegisterWorkflow(PaymentWorkflow)
	desc, err := c.DescribeWorkflowExecution(ctx, id, "")

	if err != nil {
		return err
	}

	exec := desc.WorkflowExecutionInfo.Execution

	if err := r.ReplayWorkflowHistoryWithOptions(nil, h, worker.ReplayWorkflowHistoryOptions{
		OriginalExecution: workflow.Execution{
			ID:    exec.WorkflowId,
			RunID: exec.RunId,
		},
	}); err != nil {
		return fmt.Errorf("history replay: %w", err)
	}

	return nil
}
