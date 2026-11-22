package config

import "testing"

// Test files end in _test.go and are only compiled by "go test".
// A test is any function named TestXxx that takes *testing.T.

// setRequired sets the variables every test needs.
func setRequired(t *testing.T) {
	t.Helper()
	// t.Setenv sets the variable for this test only and restores it afterwards.
	t.Setenv("POSTGRES_DSN", "postgres://example")
	t.Setenv("REDIS_ENDPOINT", "localhost:6379")
}

func TestLoadDefaults(t *testing.T) {
	setRequired(t)
	t.Setenv("PORT", "")
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("ANTHROPIC_MODEL", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned an unexpected error: %v", err)
	}
	if cfg.Port != "8080" {
		t.Errorf("Port = %q, want %q", cfg.Port, "8080")
	}
	if cfg.AnthropicModel != DefaultModel {
		t.Errorf("AnthropicModel = %q, want %q", cfg.AnthropicModel, DefaultModel)
	}
	// No key is valid: the service falls back to the fake predictor.
	if cfg.AnthropicAPIKey != "" {
		t.Errorf("AnthropicAPIKey = %q, want empty", cfg.AnthropicAPIKey)
	}
}

func TestLoadModelGateway(t *testing.T) {
	setRequired(t)
	t.Setenv("ANTHROPIC_API_KEY", "thk_test")
	t.Setenv("ANTHROPIC_BASE_URL", "https://tokenharbor.ai")
	t.Setenv("ANTHROPIC_MODEL", "mimo-v2.6-flash:free")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned an unexpected error: %v", err)
	}
	if cfg.AnthropicBaseURL != "https://tokenharbor.ai" || cfg.AnthropicModel != "mimo-v2.6-flash:free" {
		t.Errorf("gateway settings = %q %q, want the values from the environment",
			cfg.AnthropicBaseURL, cfg.AnthropicModel)
	}
}

func TestLoadFailsWithoutPostgresDSN(t *testing.T) {
	setRequired(t)
	t.Setenv("POSTGRES_DSN", "")

	if _, err := Load(); err == nil {
		t.Fatal("Load() returned no error, want one for the missing POSTGRES_DSN")
	}
}
