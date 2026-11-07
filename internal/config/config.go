// Package config loads the workshop's environment-based service settings.
package config

import (
	"os"
	"strconv"

	"devfest26/internal/domain"
)

// Config holds the service addresses, Temporal routing, Gemini settings and
// transfer limit read by the workshop entry points. Load applies shared defaults
// so the API and worker use the same task queue and Workflow ID prefix.
type Config struct {
	// API is the loopback host:port on which the agent HTTP API listens.
	API string

	// Payment is the simulated provider's loopback HTTP host:port.
	Payment string

	// DSN is the configured PostgreSQL connection URL.
	DSN string

	// Temporal is the Temporal service's gRPC host:port.
	Temporal string

	// Queue routes Workflow and Activity tasks to the worker.
	Queue string

	// Prefix namespaces Workflow IDs for this workshop environment.
	Prefix string

	// Model names the Gemini model used for agent reasoning.
	Model string

	// Audit is the JSONL file for model invocation identifiers.
	Audit string

	// Limit is the maximum amount of one transfer, in integer kobo.
	Limit int64
}

func env(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}

	return d
}

func Load() Config {
	limit := domain.DefaultLimitKobo

	if s := os.Getenv("TRANSFER_LIMIT_KOBO"); s != "" {
		if n, e := strconv.ParseInt(s, 10, 64); e == nil && n > 0 {
			limit = n
		}
	}

	prefix := env("TEMPORAL_TASK_QUEUE", "devfest26-workshop")
	dsn := env("DATABASE_URL", env("PAYMENT_DATABASE_URL", ""))

	return Config{
		API:      env("API_ADDRESS", "127.0.0.1:8090"),
		Payment:  env("PAYMENT_ADDRESS", "127.0.0.1:8091"),
		DSN:      dsn,
		Temporal: env("TEMPORAL_ADDRESS", "127.0.0.1:7233"),
		Queue:    prefix,
		Prefix:   prefix,
		Model:    env("GEMINI_MODEL", "gemini-3.6-flash"),
		Audit:    env("MODEL_AUDIT_PATH", ".local/model-invocations.jsonl"),
		Limit:    limit,
	}
}
