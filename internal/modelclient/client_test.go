package modelclient

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAliyunLoadsFromDotEnv(t *testing.T) {
	clearEnvForTest(t, "DASHSCOPE_API_KEY")
	clearEnvForTest(t, "DASHSCOPE_BASE_URL")

	path := filepath.Join(t.TempDir(), ".env")
	data := []byte(`
DASHSCOPE_API_KEY=test-api-key
DASHSCOPE_BASE_URL=https://example.com/v1
`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Aliyun().LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}

	if cfg.Provider != ProviderAliyun {
		t.Fatalf("Provider = %q, want %q", cfg.Provider, ProviderAliyun)
	}
	if cfg.APIKey != "test-api-key" {
		t.Fatalf("APIKey = %q, want %q", cfg.APIKey, "test-api-key")
	}
	if cfg.BaseURL != "https://example.com/v1" {
		t.Fatalf("BaseURL = %q, want %q", cfg.BaseURL, "https://example.com/v1")
	}
}

func TestAliyunUsesDefaultBaseURL(t *testing.T) {
	clearEnvForTest(t, "DASHSCOPE_BASE_URL")
	t.Setenv("DASHSCOPE_API_KEY", "test-api-key")

	cfg, err := Aliyun().LoadConfig(filepath.Join(t.TempDir(), ".env"))
	if err != nil {
		t.Fatal(err)
	}

	if cfg.BaseURL != DefaultAliyunBaseURL {
		t.Fatalf("BaseURL = %q, want %q", cfg.BaseURL, DefaultAliyunBaseURL)
	}
}

func TestOpenAILoadsWithoutDefaultBaseURL(t *testing.T) {
	clearEnvForTest(t, "OPENAI_BASE_URL")
	t.Setenv("OPENAI_API_KEY", "test-api-key")

	cfg, err := OpenAI().LoadConfig(filepath.Join(t.TempDir(), ".env"))
	if err != nil {
		t.Fatal(err)
	}

	if cfg.Provider != ProviderOpenAI {
		t.Fatalf("Provider = %q, want %q", cfg.Provider, ProviderOpenAI)
	}
	if cfg.BaseURL != "" {
		t.Fatalf("BaseURL = %q, want empty", cfg.BaseURL)
	}
}

func TestNewEnvAdapterLoadsConfig(t *testing.T) {
	t.Setenv("MOONSHOT_API_KEY", "test-api-key")
	clearEnvForTest(t, "MOONSHOT_BASE_URL")

	adapter := NewEnvAdapter("moonshot", "MOONSHOT_API_KEY", "MOONSHOT_BASE_URL", "https://api.moonshot.cn/v1")
	cfg, err := adapter.LoadConfig(filepath.Join(t.TempDir(), ".env"))
	if err != nil {
		t.Fatal(err)
	}

	if cfg.Provider != "moonshot" {
		t.Fatalf("Provider = %q, want %q", cfg.Provider, "moonshot")
	}
	if cfg.BaseURL != "https://api.moonshot.cn/v1" {
		t.Fatalf("BaseURL = %q, want %q", cfg.BaseURL, "https://api.moonshot.cn/v1")
	}
}

func TestLoadConfigRequiresAPIKey(t *testing.T) {
	clearEnvForTest(t, "CUSTOM_API_KEY")

	adapter := NewEnvAdapter("custom", "CUSTOM_API_KEY", "CUSTOM_BASE_URL", "")
	if _, err := adapter.LoadConfig(filepath.Join(t.TempDir(), ".env")); err == nil {
		t.Fatal("expected missing API key error")
	}
}

func TestLoadConfigRequiresProvider(t *testing.T) {
	t.Setenv("CUSTOM_API_KEY", "test-api-key")

	adapter := NewEnvAdapter("", "CUSTOM_API_KEY", "CUSTOM_BASE_URL", "")
	if _, err := adapter.LoadConfig(filepath.Join(t.TempDir(), ".env")); err == nil {
		t.Fatal("expected missing provider error")
	}
}

func TestRequestOptionsRequiresAPIKey(t *testing.T) {
	if _, err := RequestOptions(Config{Provider: ProviderAliyun}); err == nil {
		t.Fatal("expected missing API key error")
	}
}

func TestNewFromEnvBuildsClient(t *testing.T) {
	t.Setenv("DASHSCOPE_API_KEY", "test-api-key")
	clearEnvForTest(t, "DASHSCOPE_BASE_URL")

	client, cfg, err := NewFromEnv(Aliyun(), filepath.Join(t.TempDir(), ".env"))
	if err != nil {
		t.Fatal(err)
	}

	if cfg.BaseURL != DefaultAliyunBaseURL {
		t.Fatalf("BaseURL = %q, want %q", cfg.BaseURL, DefaultAliyunBaseURL)
	}
	_ = client
}

func TestNewFromEnvRequiresAdapter(t *testing.T) {
	if _, _, err := NewFromEnv(nil, filepath.Join(t.TempDir(), ".env")); err == nil {
		t.Fatal("expected nil adapter error")
	}
}

func clearEnvForTest(t *testing.T, key string) {
	t.Helper()

	old, hadOld := os.LookupEnv(key)
	if err := os.Unsetenv(key); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if hadOld {
			_ = os.Setenv(key, old)
			return
		}
		_ = os.Unsetenv(key)
	})
}
