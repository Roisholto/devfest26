// Command api accepts requests and communicates with Temporal; it runs no worker.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"go.temporal.io/sdk/client"

	"devfest26/internal/api"
	"devfest26/internal/config"
	"devfest26/internal/httpx"
)

func main() {
	if err := run(); err != nil {
		slog.Error("API failed", "error", err)
		os.Exit(1)
	}
}

func run() error {
	if len(os.Args) != 1 {
		return fmt.Errorf("usage: api")
	}

	if err := config.LoadDotEnv(".env"); err != nil {
		return err
	}

	cfg := config.Load()
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)

	defer cancel()

	c, err := client.Dial(client.Options{
		HostPort: cfg.Temporal,
	})

	if err != nil {
		return err
	}

	defer c.Close()

	server := &api.Server{
		Temporal: c,
		Config:   cfg,
		Payment:  httpx.NewClient("http://" + cfg.Payment),
	}

	slog.Info("API ready", "queue", cfg.Queue)

	return httpx.Serve(ctx, cfg.API, server.Handler())
}
