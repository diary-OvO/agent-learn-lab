// Package modelclient 集中管理 OpenAI-compatible client 初始化。
// AI写的
// 这个包用 Adapter 边界隔离不同模型厂商的初始化差异，让业务代码不再直接关心：
// API key 环境变量叫什么、是否需要自定义 base URL、如何拼 openai.NewClient options。
//
// 在这个包里的 Adapter 对应关系是：
//   - Target：Adapter.LoadConfig，返回统一的 Config。
//   - Adaptee：各厂商自己的环境变量约定，例如 DASHSCOPE_API_KEY。
//   - Adapter：EnvAdapter，把某个厂商的 env 约定映射成 Config。
//   - Client：NewFromEnv/NewClient，把 Config 转成 openai.NewClient options。
package modelclient

import (
	"fmt"
	"os"
	"strings"

	"AgentLoop/internal/config"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
)

// Provider 标识一个模型厂商或一类 OpenAI-compatible endpoint。
type Provider string

const (
	// ProviderOpenAI 表示官方 OpenAI API。
	ProviderOpenAI Provider = "openai"
	// ProviderAliyun 表示阿里云 DashScope 的 OpenAI-compatible endpoint。
	ProviderAliyun Provider = "aliyun"

	// DefaultAliyunBaseURL 是 DashScope 兼容 OpenAI 协议的默认 endpoint。
	DefaultAliyunBaseURL = "https://dashscope.aliyuncs.com/compatible-mode/v1"
)

// Config 是业务层使用的统一 client 初始化配置。
//
// 不同厂商 adapter 都应该返回这个结构。这样上层不用知道各厂商 API key 环境变量名、
// base URL 默认值、以及 openai-go option 该如何组装。
type Config struct {
	Provider Provider
	APIKey   string
	BaseURL  string
}

// Adapter 是模型 client 初始化的 Target 接口。
//
// 新增一个厂商 adapter 时，只需要实现 LoadConfig。它可以从环境变量、密钥系统、
// 配置文件等任意来源读取信息，最后归一化成 Config。
type Adapter interface {
	LoadConfig(paths ...string) (Config, error)
}

// EnvAdapter 把某个厂商的环境变量约定适配成统一 Config。
//
// 对 OpenAI-compatible provider 来说，大部分初始化差异都集中在 API key env 和
// base URL 上，因此这个通用 adapter 足够覆盖很多公司。
type EnvAdapter struct {
	Provider       Provider
	APIKeyEnv      string
	BaseURLEnv     string
	DefaultBaseURL string
}

var _ Adapter = EnvAdapter{}

// OpenAI 返回官方 OpenAI API 的 env adapter。
//
// OPENAI_BASE_URL 可选。为空时，openai-go 会使用 SDK 默认地址。
func OpenAI() EnvAdapter {
	return NewEnvAdapter(ProviderOpenAI, "OPENAI_API_KEY", "OPENAI_BASE_URL", "")
}

// Aliyun 返回 DashScope OpenAI-compatible API 的 env adapter。
//
// DASHSCOPE_BASE_URL 可选。为空时使用 DefaultAliyunBaseURL。
func Aliyun() EnvAdapter {
	return NewEnvAdapter(ProviderAliyun, "DASHSCOPE_API_KEY", "DASHSCOPE_BASE_URL", DefaultAliyunBaseURL)
}

// NewEnvAdapter 创建一个基于环境变量的 OpenAI-compatible provider adapter。
//
// 如果某个厂商不支持通过 env 覆盖 base URL，可以让 baseURLEnv 为空。
// 如果希望使用 openai-go 默认 endpoint，可以让 defaultBaseURL 为空。
func NewEnvAdapter(provider Provider, apiKeyEnv, baseURLEnv, defaultBaseURL string) EnvAdapter {
	return EnvAdapter{
		Provider:       provider,
		APIKeyEnv:      apiKeyEnv,
		BaseURLEnv:     baseURLEnv,
		DefaultBaseURL: defaultBaseURL,
	}
}

// CustomEnvProvider 创建一个基于环境变量的 OpenAI-compatible provider adapter。
//
// Deprecated: use NewEnvAdapter. 保留这个别名是为了让早期示例继续可编译。
func CustomEnvProvider(provider Provider, apiKeyEnv, baseURLEnv, defaultBaseURL string) EnvAdapter {
	return NewEnvAdapter(provider, apiKeyEnv, baseURLEnv, defaultBaseURL)
}

// LoadConfig 加载 .env，读取当前厂商需要的环境变量，并返回 NewClient 需要的统一 Config。
//
// config.LoadDotEnv 会保留已有进程环境变量，因此真实环境变量优先级高于 .env 文件。
func (a EnvAdapter) LoadConfig(paths ...string) (Config, error) {
	if err := config.LoadDotEnv(paths...); err != nil {
		return Config{}, err
	}

	provider := strings.TrimSpace(string(a.Provider))
	if provider == "" {
		return Config{}, fmt.Errorf("model client adapter has no provider configured")
	}

	apiKeyEnv := strings.TrimSpace(a.APIKeyEnv)
	if apiKeyEnv == "" {
		return Config{}, fmt.Errorf("%s adapter has no API key env configured", a.providerName())
	}

	apiKey := strings.TrimSpace(os.Getenv(apiKeyEnv))
	if apiKey == "" {
		return Config{}, fmt.Errorf("%s is not set; configure it in .env or the process environment", apiKeyEnv)
	}

	baseURL := strings.TrimSpace(a.DefaultBaseURL)
	if baseURLEnv := strings.TrimSpace(a.BaseURLEnv); baseURLEnv != "" {
		if value := strings.TrimSpace(os.Getenv(baseURLEnv)); value != "" {
			baseURL = value
		}
	}

	return Config{
		Provider: Provider(provider),
		APIKey:   apiKey,
		BaseURL:  baseURL,
	}, nil
}

// NewFromEnv 通过 Adapter 加载 Config，并创建 openai-go client。
//
// 返回 Config 是为了方便日志诊断和测试；注意不要把 Config.APIKey 打进日志。
func NewFromEnv(adapter Adapter, paths ...string) (openai.Client, Config, error) {
	if adapter == nil {
		return openai.Client{}, Config{}, fmt.Errorf("model client adapter is nil")
	}

	cfg, err := adapter.LoadConfig(paths...)
	if err != nil {
		return openai.Client{}, Config{}, err
	}

	client, err := NewClient(cfg)
	if err != nil {
		return openai.Client{}, Config{}, err
	}

	return client, cfg, nil
}

// NewClient 把统一 Config 转成 openai-go client。
func NewClient(cfg Config) (openai.Client, error) {
	opts, err := RequestOptions(cfg)
	if err != nil {
		return openai.Client{}, err
	}

	return openai.NewClient(opts...), nil
}

// RequestOptions 把统一 Config 转成 openai-go request options。
//
// 拆出这个函数后，测试可以验证 option 构建逻辑，而不用发起真实网络请求。
func RequestOptions(cfg Config) ([]option.RequestOption, error) {
	apiKey := strings.TrimSpace(cfg.APIKey)
	if apiKey == "" {
		return nil, fmt.Errorf("%s API key is empty", providerName(cfg.Provider))
	}

	opts := []option.RequestOption{
		option.WithAPIKey(apiKey),
	}

	if baseURL := strings.TrimSpace(cfg.BaseURL); baseURL != "" {
		opts = append(opts, option.WithBaseURL(baseURL))
	}

	return opts, nil
}

func (a EnvAdapter) providerName() string {
	return providerName(a.Provider)
}

func providerName(provider Provider) string {
	if strings.TrimSpace(string(provider)) == "" {
		return "model provider"
	}
	return string(provider)
}
