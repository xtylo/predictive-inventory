// Package config loads the service configuration from environment variables.
//
// It lives under internal/, a folder name the Go compiler treats specially:
// packages inside it can only be imported by code in this same module.
package config

import (
	"fmt"
	"os"
)

// DefaultModel is used when ANTHROPIC_MODEL is not set.
const DefaultModel = "claude-haiku-5-5"

// Config holds every setting the service needs at startup.
//
// Names starting with an uppercase letter are exported (public);
// lowercase names are private to the package. There are no keywords for it.
type Config struct {
	Port          string
	PostgresDSN   string
	RedisEndpoint string

	// The ANTHROPIC_* names follow the convention of Anthropic's SDKs and
	// tools, so gateways documented for them work here unchanged.
	// An empty AnthropicAPIKey selects the fake predictor.
	AnthropicAPIKey  string
	AnthropicBaseURL string
	AnthropicModel   string

	// ReorderQueueURL is the SQS FIFO queue for reorder messages.
	// Empty disables publishing.
	ReorderQueueURL string
}

// Load reads the environment and returns a validated Config.
//
// Returning (value, error) is the standard Go signature for anything that
// can fail. The service should refuse to start with a broken configuration
// instead of failing later on the first request.
func Load() (Config, error) {
	cfg := Config{
		Port:          getEnv("PORT", "8080"),
		PostgresDSN:   os.Getenv("POSTGRES_DSN"),
		RedisEndpoint: os.Getenv("REDIS_ENDPOINT"),

		AnthropicAPIKey:  os.Getenv("ANTHROPIC_API_KEY"),
		AnthropicBaseURL: os.Getenv("ANTHROPIC_BASE_URL"),
		AnthropicModel:   getEnv("ANTHROPIC_MODEL", DefaultModel),

		ReorderQueueURL: os.Getenv("SQS_REORDER_QUEUE_URL"),
	}

	if cfg.PostgresDSN == "" {
		return Config{}, fmt.Errorf("POSTGRES_DSN is required")
	}
	if cfg.RedisEndpoint == "" {
		return Config{}, fmt.Errorf("REDIS_ENDPOINT is required")
	}

	return cfg, nil
}

// getEnv returns the value of key, or fallback when it is unset or empty.
func getEnv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
