package app

import (
	"errors"
	"fmt"
	"os"

	"github.com/joho/godotenv"
)

// Environment contains the supported environment configuration. Empty values
// disable the webhook or leave the default check interval in place.
type Environment struct {
	DiscordWebhookURL string
	CheckInterval     string
}

// LoadEnvironment reads an optional .env file without changing process state.
// Explicit process variables take precedence, including explicitly empty values.
func LoadEnvironment(path string, lookup func(string) (string, bool)) (Environment, error) {
	values := make(map[string]string)
	file, err := os.Open(path)
	if err == nil {
		defer file.Close()
		values, err = godotenv.Parse(file)
		if err != nil {
			// Parser errors may include the source line containing webhook secrets.
			return Environment{}, fmt.Errorf("invalid .env file: expected KEY=value entries")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return Environment{}, fmt.Errorf("read .env file: %w", err)
	}
	get := func(key string) string {
		if value, exists := lookup(key); exists {
			return value
		}
		return values[key]
	}
	return Environment{
		DiscordWebhookURL: get("DISCORD_WEBHOOK_URL"),
		CheckInterval:     get("CHECK_INTERVAL"),
	}, nil
}
