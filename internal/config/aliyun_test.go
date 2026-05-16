package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadAliyunEnvConfigLoadsFromDotEnv(t *testing.T) {
	clearEnvForTest(t, "DASHSCOPE_API_KEY")
	clearEnvForTest(t, "DASHSCOPE_BASE_URL")
	t.Chdir(t.TempDir())

	data := []byte(`
DASHSCOPE_API_KEY=test-api-key
DASHSCOPE_BASE_URL=https://example.com/v1
`)
	if err := os.WriteFile(filepath.Join(".", ".env"), data, 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadAliyunEnvConfig()
	if err != nil {
		t.Fatal(err)
	}

	if cfg.APIKey != "test-api-key" {
		t.Fatalf("APIKey = %q, want %q", cfg.APIKey, "test-api-key")
	}
	if cfg.BaseURL != "https://example.com/v1" {
		t.Fatalf("BaseURL = %q, want %q", cfg.BaseURL, "https://example.com/v1")
	}
}

func TestLoadAliyunEnvConfigUsesDefaultBaseURL(t *testing.T) {
	clearEnvForTest(t, "DASHSCOPE_BASE_URL")
	t.Chdir(t.TempDir())
	t.Setenv("DASHSCOPE_API_KEY", "test-api-key")

	cfg, err := LoadAliyunEnvConfig()
	if err != nil {
		t.Fatal(err)
	}

	if cfg.BaseURL != defaultDashScopeBaseURL {
		t.Fatalf("BaseURL = %q, want %q", cfg.BaseURL, defaultDashScopeBaseURL)
	}
}

func TestLoadAliyunEnvConfigRequiresAPIKey(t *testing.T) {
	clearEnvForTest(t, "DASHSCOPE_API_KEY")
	clearEnvForTest(t, "DASHSCOPE_BASE_URL")
	t.Chdir(t.TempDir())

	if _, err := LoadAliyunEnvConfig(); err == nil {
		t.Fatal("expected missing DASHSCOPE_API_KEY error")
	}
}
