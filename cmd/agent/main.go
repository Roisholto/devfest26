// Command agent runs the REST API and Temporal worker in one process.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"go.temporal.io/sdk/client"
	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"

	"devfest26/internal/agent"
	"devfest26/internal/api"
	"devfest26/internal/config"
	"devfest26/internal/httpx"
)

func main() {
	if err := run(); err != nil {
		slog.Error("agent failed", "error", err)
		os.Exit(1)
	}
}

func run() error {
	if err := config.LoadDotEnv(".env"); err != nil {
		return err
	}

	cfg := config.Load()

	if cfg.Instance != "participant" && cfg.Instance != "presenter" {
		return fmt.Errorf("WORKSHOP_INSTANCE must be presenter or participant")
	}

	if cfg.Mode != "live" && cfg.Mode != "scripted" {
		return fmt.Errorf("MODEL_MODE must be live or scripted")
	}

	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "preflight":
			if len(os.Args) != 2 {
				return fmt.Errorf("usage: agent preflight")
			}
		case "replay":
			if len(os.Args) < 3 || len(os.Args) > 4 {
				return fmt.Errorf("usage: agent replay WORKFLOW_ID [history-output.json]")
			}
		default:
			return fmt.Errorf("usage: agent [preflight|replay WORKFLOW_ID [history-output.json]]")
		}
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)

	defer cancel()

	c, err := client.Dial(client.Options{
		HostPort: cfg.Temporal,
	})

	if err != nil {
		return err
	}

	defer c.Close()

	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "replay":
			path := ""

			if len(os.Args) == 4 {
				path = os.Args[3]
			}

			if err := agent.Replay(ctx, c, os.Args[2], path); err != nil {
				return err
			}

			fmt.Println("History replay passed:", os.Args[2])

			return nil
		case "preflight":
			checkCtx, done := context.WithTimeout(ctx, 45*time.Second)

			defer done()

			if _, err := c.CheckHealth(checkCtx, &client.CheckHealthRequest{}); err != nil {
				return err
			}

			if cfg.Mode == "live" {
				if os.Getenv("GEMINI_API_KEY") == "" && os.Getenv("GOOGLE_API_KEY") == "" {
					return fmt.Errorf("live mode needs GEMINI_API_KEY or GOOGLE_API_KEY")
				}

				llm, err := agent.NewGemini(checkCtx, cfg.Model)

				if err != nil {
					return err
				}

				for _, err := range llm.GenerateContent(checkCtx, &model.LLMRequest{
					Model: cfg.Model,
					Contents: []*genai.Content{
						genai.NewContentFromText("Reply with OK", genai.RoleUser),
					},
				}, false) {
					if err != nil {
						return fmt.Errorf("live Gemini preflight: %w", err)
					}
				}
			}

			fmt.Printf("AGENT READY instance=%s mode=%s queue=%s\n", cfg.Instance, cfg.Mode, cfg.Queue)

			return nil
		}
	}

	if err := os.MkdirAll(filepath.Dir(cfg.Audit), 0700); err != nil {
		return err
	}

	stopWorker, err := agent.StartWorker(c, cfg)

	if err != nil {
		return err
	}

	defer stopWorker()

	server := &api.Server{
		Temporal: c,
		Config:   cfg,
		Payment:  httpx.NewClient("http://" + cfg.Payment),
	}

	slog.Info("agent ready", "queue", cfg.Queue, "mode", cfg.Mode)

	return httpx.Serve(ctx, cfg.API, server.Handler())
}
