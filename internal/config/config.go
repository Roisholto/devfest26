package config

import (
	"os"
	"strconv"

	"devfest26/internal/domain"
)

type Config struct {
	Instance, Checkpoint, API, Payment, DSN, Temporal, Queue, Prefix, Mode, Model, Audit string
	Limit                                                                                int64
}

func env(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}
func Load() Config {
	i := env("WORKSHOP_INSTANCE", "participant")
	checkpoint := env("WORKSHOP_CHECKPOINT", "main")
	api, pay := "127.0.0.1:8090", "127.0.0.1:8091"
	if i == "presenter" {
		api, pay = "127.0.0.1:8080", "127.0.0.1:8081"
	}
	limit := domain.DefaultLimitKobo
	if s := os.Getenv("TRANSFER_LIMIT_KOBO"); s != "" {
		if n, e := strconv.ParseInt(s, 10, 64); e == nil && n > 0 {
			limit = n
		}
	}
	prefix := i + "-" + checkpoint
	return Config{Instance: i, Checkpoint: checkpoint, API: env("API_ADDRESS", api), Payment: env("PAYMENT_ADDRESS", pay), DSN: env("PAYMENT_DATABASE_URL", "postgres://devfest26_"+i+"@127.0.0.1:55432/devfest26_"+i+"?sslmode=disable"), Temporal: env("TEMPORAL_ADDRESS", "127.0.0.1:7233"), Queue: prefix, Prefix: prefix, Mode: env("MODEL_MODE", "scripted"), Model: env("GEMINI_MODEL", "gemini-3.6-flash"), Audit: env("MODEL_AUDIT_PATH", ".local/"+i+"/model-invocations.jsonl"), Limit: limit}
}
