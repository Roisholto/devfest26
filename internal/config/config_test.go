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
	t.Setenv("MODEL_MODE", "scripted")
	p := filepath.Join(t.TempDir(), ".env")
	data := "DATABASE_URL='postgres://demo:quoted%24password@localhost/example?sslmode=disable&connect_timeout=3'\nMODEL_MODE=live\nHOME=/must-not-change\n"

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

	if Load().Mode != "scripted" || os.Getenv("HOME") != home {
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
	t.Setenv("WORKSHOP_INSTANCE", "presenter")
	t.Setenv("PG_WORKSHOP_PORT", "55433")

	if Load().DSN != "postgres://devfest26_presenter@127.0.0.1:55433/devfest26_presenter?sslmode=disable" {
		t.Fatal("managed database default is inconsistent")
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

func TestQueuesAreFixedByInstanceAndCanBeIsolatedForVerification(t *testing.T) {
	t.Setenv("TEMPORAL_TASK_QUEUE", "")
	t.Setenv("WORKSHOP_INSTANCE", "participant")

	participant := Load()

	if participant.Queue != "devfest26-participant" || participant.Prefix != participant.Queue {
		t.Fatalf("unexpected participant queue: %s", participant.Queue)
	}

	t.Setenv("WORKSHOP_INSTANCE", "presenter")

	if Load().Queue != "devfest26-presenter" {
		t.Fatal("presenter must use its own fixed queue")
	}

	t.Setenv("TEMPORAL_TASK_QUEUE", "devfest26-check-isolated")

	isolated := Load()

	if isolated.Queue != "devfest26-check-isolated" || isolated.Prefix != isolated.Queue {
		t.Fatal("verification must isolate both polling and workflow ownership")
	}
}
