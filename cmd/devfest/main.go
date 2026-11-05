package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
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
	"devfest26/internal/payment"
)

func main() {
	if err := run(); err != nil {
		slog.Error("command failed", "error", err)
		os.Exit(1)
	}
}

func run() error {
	if len(os.Args) < 2 {
		return fmt.Errorf("usage: devfest payment|agent|migrate|seed|preflight|evidence|database-check|database-init")
	}

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

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)

	defer cancel()

	switch os.Args[1] {
	case "database-init":
		dbctx, done := context.WithTimeout(ctx, 12*time.Second)

		defer done()

		if err := payment.InitDatabase(dbctx, cfg.DSN); err != nil {
			return err
		}

		fmt.Println("Configured PostgreSQL database is ready")

		return nil
	case "replay":
		if len(os.Args) < 3 {
			return fmt.Errorf("usage: devfest replay WORKFLOW_ID [history-output.json]")
		}

		c, err := client.Dial(client.Options{
			HostPort: cfg.Temporal,
		})

		if err != nil {
			return err
		}

		defer c.Close()

		path := ""

		if len(os.Args) > 3 {
			path = os.Args[3]
		}

		if err = agent.Replay(ctx, c, os.Args[2], path); err != nil {
			return err
		}

		fmt.Println("History replay passed:", os.Args[2])

		return nil
	case "payment", "migrate", "seed", "preflight", "evidence", "database-check":
		dbctx, done := context.WithTimeout(ctx, 8*time.Second)
		store, err := payment.Open(dbctx, cfg.DSN, cfg.Limit)
		done()

		if err != nil {
			return payment.ConnectionError(err)
		}

		defer store.Pool.Close()

		switch os.Args[1] {
		case "database-check":
			fmt.Println("PostgreSQL connection verified; no schema or data changes made")

			return nil
		case "migrate":
			return store.Migrate(ctx)
		case "seed":
			return store.Seed(ctx, cfg.Instance == "presenter")
		case "evidence":
			e, err := store.Evidence(ctx)
			fmt.Printf("%+v\n", e)

			return err
		case "preflight":
			if _, err = store.Evidence(ctx); err != nil {
				return fmt.Errorf("migrate and seed first: %w", err)
			}

			c, err := client.Dial(client.Options{
				HostPort: cfg.Temporal,
			})

			if err != nil {
				return err
			}

			defer c.Close()

			if _, err = c.CheckHealth(ctx, &client.CheckHealthRequest{}); err != nil {
				return err
			}

			if cfg.Mode == "live" {
				if os.Getenv("GEMINI_API_KEY") == "" && os.Getenv("GOOGLE_API_KEY") == "" {
					return fmt.Errorf("live mode needs GEMINI_API_KEY or GOOGLE_API_KEY")
				}

				m, err := agent.NewGemini(ctx, cfg.Model)

				if err != nil {
					return err
				}

				for _, err := range m.GenerateContent(ctx, &model.LLMRequest{
					Model:    cfg.Model,
					Contents: []*genai.Content{genai.NewContentFromText("Reply with OK", genai.RoleUser)},
				}, false) {
					if err != nil {
						return fmt.Errorf("live Gemini preflight: %w", err)
					}
				}
			}

			fmt.Printf("READY instance=%s mode=%s queue=%s\n", cfg.Instance, cfg.Mode, cfg.Queue)

			return nil
		default:
			return serve(ctx, cfg.Payment, payment.Handler(store))
		}
	case "agent":
		if err := os.MkdirAll(filepath.Dir(cfg.Audit), 0700); err != nil {
			return err
		}

		c, err := client.Dial(client.Options{
			HostPort: cfg.Temporal,
		})

		if err != nil {
			return err
		}

		defer c.Close()

		stopWorker, err := agent.StartWorker(c, cfg)

		if err != nil {
			return err
		}

		defer stopWorker()

		s := &api.Server{
			Temporal: c,
			Config:   cfg,
			Payment:  httpx.NewClient("http://" + cfg.Payment),
		}

		slog.Info("agent ready", "queue", cfg.Queue, "mode", cfg.Mode)

		return serve(ctx, cfg.API, s.Handler())
	}

	return fmt.Errorf("unknown command %q", os.Args[1])
}

func serve(ctx context.Context, address string, h http.Handler) error {
	host, _, err := net.SplitHostPort(address)

	if err != nil || !(host == "127.0.0.1" || host == "localhost" || host == "::1") {
		return fmt.Errorf("workshop services must bind to localhost")
	}

	s := &http.Server{
		Addr:              address,
		Handler:           h,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      20 * time.Second,
		IdleTimeout:       30 * time.Second,
	}

	go func() {
		<-ctx.Done()
		c, cancel := context.WithTimeout(context.Background(), 3*time.Second)

		defer cancel()

		_ = s.Shutdown(c)
	}()

	slog.Info("HTTP service ready", "address", address)

	if err = s.ListenAndServe(); err != http.ErrServerClosed {
		return err
	}

	return nil
}
