package agent

import (
	"context"
	"time"

	"go.temporal.io/googleadk"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"
	"google.golang.org/adk/v2/model"

	"devfest26/internal/config"
	"devfest26/internal/httpx"
)

// StartWorker shares the agent process and Temporal client with its REST API.
// The returned function stops polling and releases the ADK Activity resources.
func StartWorker(c client.Client, cfg config.Config) (func(), error) {
	factories := map[string]googleadk.ModelFactory{
		"scripted": func(context.Context, string) (model.LLM, error) {
			return AuditedModel{
				Inner: ScriptedModel{},
				Path:  cfg.Audit,
			}, nil
		},
		cfg.Model: func(ctx context.Context, name string) (model.LLM, error) {
			llm, err := NewGemini(ctx, name)

			if err != nil {
				return nil, err
			}

			return AuditedModel{
				Inner: llm,
				Path:  cfg.Audit,
			}, nil
		},
	}

	activities, err := googleadk.NewActivities(googleadk.Config{
		Models: factories,
	})

	if err != nil {
		return nil, err
	}

	w := worker.New(c, cfg.Queue, worker.Options{
		WorkerStopTimeout: 2 * time.Second,
	})
	activities.Register(w)
	w.RegisterWorkflow(AgentWorkflow)
	w.RegisterWorkflow(PaymentWorkflow)

	router := &Router{
		Client: httpx.NewClient("http://" + cfg.Payment),
		Limit:  cfg.Limit,
	}
	w.RegisterDynamicActivity(router.Execute, activity.DynamicRegisterOptions{})

	// Internal payment operations are absent from the model's tool catalog.
	w.RegisterActivity(&PaymentActivities{
		Client: httpx.NewClient("http://" + cfg.Payment),
	})

	if err := w.Start(); err != nil {
		activities.Close()

		return nil, err
	}

	return func() {
		w.Stop()
		activities.Close()
	}, nil
}
