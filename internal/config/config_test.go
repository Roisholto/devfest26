package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func unset(t *testing.T, key string) {
	t.Helper()
	old, exists := os.LookupEnv(key)
	_ = os.Unsetenv(key)
	t.Cleanup(func() {
		if exists {
			_ = os.Setenv(key, old)
		} else {
			_ = os.Unsetenv(key)
		}
	})
}

func TestDotEnvLoadsDatabaseURLWithoutOverridingProcess(t *testing.T) {
	unset(t, "DATABASE_URL")
	unset(t, "PAYMENT_DATABASE_URL")
	t.Setenv("GEMINI_MODEL", "exported-model")
	p := filepath.Join(t.TempDir(), ".env")
	data := "DATABASE_URL='postgres://demo:quoted%24password@localhost/example?sslmode=disable&connect_timeout=3'\nGEMINI_MODEL=file-model\nHOME=/must-not-change\n"

	if err := os.WriteFile(p, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}

	home := os.Getenv("HOME")

	if err := LoadDotEnv(p); err != nil {
		t.Fatal(err)
	}

	if Load().DSN != "postgres://demo:quoted%24password@localhost/example?sslmode=disable&connect_timeout=3" {
		t.Fatal("DATABASE_URL not loaded exactly")
	}

	if Load().Model != "exported-model" || os.Getenv("HOME") != home {
		t.Fatal("dotenv overrode process/system environment")
	}

	t.Setenv("DATABASE_URL", "postgres://explicit/override")

	if err := LoadDotEnv(p); err != nil {
		t.Fatal(err)
	}

	if Load().DSN != "postgres://explicit/override" {
		t.Fatal("file overrode exported DATABASE_URL")
	}
}

func TestDatabaseURLPrecedenceAndLegacyFallback(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://canonical/db")
	t.Setenv("PAYMENT_DATABASE_URL", "postgres://legacy/db")

	if Load().DSN != "postgres://canonical/db" {
		t.Fatal("canonical key must win")
	}

	t.Setenv("DATABASE_URL", "")

	if Load().DSN != "postgres://legacy/db" {
		t.Fatal("legacy fallback missing")
	}

	t.Setenv("PAYMENT_DATABASE_URL", "")

	if Load().DSN != "" {
		t.Fatal("database configuration must be explicit")
	}
}

func TestMissingAndMalformedDotEnv(t *testing.T) {
	p := filepath.Join(t.TempDir(), ".env")

	if err := LoadDotEnv(p); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(p, []byte("DATABASE_URL='super-secret-credential"), 0600); err != nil {
		t.Fatal(err)
	}

	err := LoadDotEnv(p)

	if err == nil || strings.Contains(err.Error(), "super-secret") {
		t.Fatal("malformed dotenv must fail without exposing the input")
	}
}

func TestWorkshopQueueAndVerificationIsolation(t *testing.T) {
	t.Setenv("TEMPORAL_TASK_QUEUE", "")

	workshop := Load()

	if workshop.Queue != "devfest26-workshop" || workshop.Prefix != workshop.Queue {
		t.Fatalf("unexpected workshop queue: %s", workshop.Queue)
	}

	t.Setenv("TEMPORAL_TASK_QUEUE", "devfest26-check-isolated")

	isolated := Load()

	if isolated.Queue != "devfest26-check-isolated" || isolated.Prefix != isolated.Queue {
		t.Fatal("verification must isolate both polling and workflow ownership")
	}
}
