// 这是ai写的
package config

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// LoadDotEnv loads KEY=VALUE pairs from .env files into the process
// environment. Existing environment variables take precedence.
func LoadDotEnv(paths ...string) error {
	if len(paths) == 0 {
		paths = []string{".env"}
	}

	for _, path := range paths {
		if err := loadDotEnvFile(path); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return err
		}
	}

	return nil
}

func loadDotEnvFile(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		raw := scanner.Text()
		if lineNo == 1 {
			raw = strings.TrimPrefix(raw, "\ufeff")
		}

		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "export ") {
			line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
		}

		key, value, ok := strings.Cut(line, "=")
		if !ok {
			return fmt.Errorf("%s:%d: invalid .env line, want KEY=VALUE", path, lineNo)
		}

		key = strings.TrimSpace(key)
		if key == "" {
			return fmt.Errorf("%s:%d: empty environment key", path, lineNo)
		}
		if _, exists := os.LookupEnv(key); exists {
			continue
		}

		parsed, err := parseDotEnvValue(strings.TrimSpace(value))
		if err != nil {
			return fmt.Errorf("%s:%d: %w", path, lineNo, err)
		}
		if err := os.Setenv(key, parsed); err != nil {
			return fmt.Errorf("%s:%d: set %s: %w", path, lineNo, key, err)
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}

	return nil
}

func parseDotEnvValue(value string) (string, error) {
	if len(value) < 2 {
		return value, nil
	}

	quote := value[0]
	if quote != '"' && quote != '\'' {
		return value, nil
	}
	if value[len(value)-1] != quote {
		return "", fmt.Errorf("unterminated quoted value")
	}

	if quote == '\'' {
		return value[1 : len(value)-1], nil
	}

	unquoted, err := strconv.Unquote(value)
	if err != nil {
		return "", fmt.Errorf("invalid quoted value: %w", err)
	}
	return unquoted, nil
}
