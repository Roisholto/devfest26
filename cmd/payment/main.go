// Command payment runs the simulated payment provider and its database tools.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"devfest26/internal/config"
	"devfest26/internal/httpx"
	"devfest26/internal/payment"
)

func main() {
	if err := run(); err != nil {
		slog.Error("payment service failed", "error", err)
		os.Exit(1)
	}
}

func run() error {
	if err := config.LoadDotEnv(".env"); err != nil {
		return err
	}

	cfg := config.Load()

	if cfg.DSN == "" {
		return fmt.Errorf("DATABASE_URL is required")
	}

	command := "serve"

	if len(os.Args) > 1 {
		command = os.Args[1]
	}

	switch command {
	case "serve", "database-init", "database-check", "migrate", "seed", "preflight", "evidence":
	default:
		return fmt.Errorf("usage: payment [serve|database-init|database-check|migrate|seed|preflight|evidence]")
	}

	if len(os.Args) > 2 {
		return fmt.Errorf("payment %s does not accept additional arguments", command)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)

	defer cancel()

	if command == "database-init" {
		dbCtx, done := context.WithTimeout(ctx, 12*time.Second)

		defer done()

		if err := payment.InitDatabase(dbCtx, cfg.DSN); err != nil {
			return err
		}

		fmt.Println("Configured PostgreSQL database is ready")

		return nil
	}

	dbCtx, done := context.WithTimeout(ctx, 8*time.Second)
	store, err := payment.Open(dbCtx, cfg.DSN, cfg.Limit)
	done()

	if err != nil {
		return payment.ConnectionError(err)
	}

	defer store.Pool.Close()

	switch command {
	case "database-check":
		fmt.Println("PostgreSQL connection verified; no schema or data changes made")

		return nil
	case "migrate":
		return store.Migrate(ctx)
	case "seed":
		return store.Seed(ctx)
	case "evidence":
		evidence, err := store.Evidence(ctx)

		if err != nil {
			return err
		}

		fmt.Printf("%+v\n", evidence)

		return nil
	case "preflight":
		if _, err := store.Evidence(ctx); err != nil {
			return fmt.Errorf("migrate and seed first: %w", err)
		}

		fmt.Println("PAYMENT READY")

		return nil
	default:
		return httpx.Serve(ctx, cfg.Payment, payment.Handler(store))
	}
}
