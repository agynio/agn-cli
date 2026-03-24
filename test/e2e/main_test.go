//go:build e2e

package e2e

import (
	"os"
	"strings"
	"testing"
)

type testConfig struct {
	Endpoint string
	APIKey   string
	Model    string
}

func TestMain(m *testing.M) {
	os.Exit(m.Run())
}

func loadTestConfig(t *testing.T) testConfig {
	t.Helper()

	endpoint := strings.TrimSpace(os.Getenv("TESTLLM_ENDPOINT"))
	if endpoint == "" {
		t.Fatal("TESTLLM_ENDPOINT is required")
	}

	return testConfig{
		Endpoint: endpoint,
		APIKey:   envOrDefault("TESTLLM_API_KEY", "e2e-dummy-key"),
		Model:    envOrDefault("TESTLLM_MODEL", "system-prompt"),
	}
}

func envOrDefault(key, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}
