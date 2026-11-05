// Command env bridges the shared dotenv parser to Bash using NUL-separated data.
// The output is consumed by scripts/env.sh, never evaluated as shell code.
package main

import (
	"fmt"
	"os"

	"devfest26/internal/config"
)

func main() {
	values, err := config.ReadDotEnv(".env")

	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	for _, key := range config.EnvKeys {
		if _, exists := os.LookupEnv(key); exists {
			continue
		}

		if value, exists := values[key]; exists {
			fmt.Printf("%s\x00%s\x00", key, value)
		}
	}
}
