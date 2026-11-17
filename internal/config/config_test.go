package config

import "testing"

// Test files end in _test.go and are only compiled by "go test".
// A test is any function named TestXxx that takes *testing.T.

func TestLoadUsesDefaultPort(t *testing.T) {
	// t.Setenv sets the variable for this test only and restores it afterwards.
	t.Setenv("PORT", "")
	t.Setenv("POSTGRES_DSN", "postgres://example")
	t.Setenv("REDIS_ENDPOINT", "localhost:6379")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned an unexpected error: %v", err)
	}
	if cfg.Port != "8080" {
		t.Errorf("Port = %q, want %q", cfg.Port, "8080")
	}
}

func TestLoadFailsWithoutPostgresDSN(t *testing.T) {
	t.Setenv("POSTGRES_DSN", "")
	t.Setenv("REDIS_ENDPOINT", "localhost:6379")

	if _, err := Load(); err == nil {
		t.Fatal("Load() returned no error, want one for the missing POSTGRES_DSN")
	}
}
