package config

import (
	"fmt"
	"os"
	"strings"

	"github.com/joho/godotenv"
)

// EnvKeys limits dotenv loading to application configuration, leaving system
// variables such as HOME, PATH and shell options untouched.
var EnvKeys = []string{
	"API_ADDRESS", "DATABASE_URL", "GEMINI_API_KEY", "GEMINI_MODEL",
	"MODEL_AUDIT_PATH", "PAYMENT_ADDRESS", "PAYMENT_DATABASE_URL",
	"TEMPORAL_ADDRESS", "TRANSFER_LIMIT_KOBO",
	"TEMPORAL_TASK_QUEUE",
}

func ReadDotEnv(path string) (map[string]string, error) {
	values, err := godotenv.Read(path)

	if os.IsNotExist(err) {
		return map[string]string{}, nil
	}

	if err != nil {
		// Parse errors can contain the original line, including credentials.
		return nil, fmt.Errorf("cannot load dotenv configuration; check file access and KEY=value syntax")
	}

	for _, key := range EnvKeys {
		if strings.IndexByte(values[key], 0) >= 0 {
			return nil, fmt.Errorf("dotenv configuration cannot contain NUL bytes")
		}
	}

	return values, nil
}

// LoadDotEnv supports direct go run execution as well as the shell launcher.
// Exported process variables take precedence over values in the file.
func LoadDotEnv(path string) error {
	values, err := ReadDotEnv(path)

	if err != nil {
		return err
	}

	for _, key := range EnvKeys {
		if _, exists := os.LookupEnv(key); exists {
			continue
		}

		if value, exists := values[key]; exists {
			if err := os.Setenv(key, value); err != nil {
				return fmt.Errorf("cannot set application configuration variable %s", key)
			}
		}
	}

	return nil
}
