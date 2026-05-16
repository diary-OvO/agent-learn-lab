// 这是ai写的
package config

import (
	"fmt"
	"os"
	"strings"
)

const defaultDashScopeBaseURL = "https://dashscope.aliyuncs.com/compatible-mode/v1"

type AliyunEnvConfig struct {
	APIKey  string
	BaseURL string
}

func LoadAliyunEnvConfig() (AliyunEnvConfig, error) {
	if err := LoadDotEnv(); err != nil {
		return AliyunEnvConfig{}, err
	}

	apiKey := strings.TrimSpace(os.Getenv("DASHSCOPE_API_KEY"))
	if apiKey == "" {
		return AliyunEnvConfig{}, fmt.Errorf("DASHSCOPE_API_KEY is not set; configure it in .env or the process environment")
	}

	baseURL := strings.TrimSpace(os.Getenv("DASHSCOPE_BASE_URL"))
	if baseURL == "" {
		baseURL = defaultDashScopeBaseURL
	}

	return AliyunEnvConfig{
		APIKey:  apiKey,
		BaseURL: baseURL,
	}, nil
}
