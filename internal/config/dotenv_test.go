package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDotEnvLoadsValuesAndKeepsExistingEnvironment(t *testing.T) {
	existingKey := "AGENT_LOOP_DOTENV_EXISTING"
	fileOnlyKey := "AGENT_LOOP_DOTENV_FILE_ONLY"
	quotedKey := "AGENT_LOOP_DOTENV_QUOTED"

	clearEnvForTest(t, fileOnlyKey)
	clearEnvForTest(t, quotedKey)
	t.Setenv(existingKey, "from-env")

	path := filepath.Join(t.TempDir(), ".env")
	data := []byte(`
# comment
AGENT_LOOP_DOTENV_EXISTING=from-file
AGENT_LOOP_DOTENV_FILE_ONLY=from-file
export AGENT_LOOP_DOTENV_QUOTED="quoted value"
`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}

	if err := LoadDotEnv(path); err != nil {
		t.Fatal(err)
	}

	if got := os.Getenv(existingKey); got != "from-env" {
		t.Fatalf("%s = %q, want %q", existingKey, got, "from-env")
	}
	if got := os.Getenv(fileOnlyKey); got != "from-file" {
		t.Fatalf("%s = %q, want %q", fileOnlyKey, got, "from-file")
	}
	if got := os.Getenv(quotedKey); got != "quoted value" {
		t.Fatalf("%s = %q, want %q", quotedKey, got, "quoted value")
	}
}

func TestLoadDotEnvIgnoresMissingDefaultFile(t *testing.T) {
	if err := LoadDotEnv(filepath.Join(t.TempDir(), ".env")); err != nil {
		t.Fatal(err)
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
