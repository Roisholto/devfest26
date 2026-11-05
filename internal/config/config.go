package config

import (
	"os"
	"strconv"

	"devfest26/internal/domain"
)

type Config struct {
	API      string
	Payment  string
	DSN      string
	Temporal string
	Queue    string
	Prefix   string
	Model    string
	Audit    string
	Limit    int64
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
