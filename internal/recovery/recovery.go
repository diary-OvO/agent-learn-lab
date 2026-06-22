package recovery

import (
	"context"
	"fmt"
	"math/rand"
	"strings"
	"time"

	"github.com/openai/openai-go/v3"
)

const (
	// DefaultMaxTokens 对标 Python DEFAULT_MAX_TOKENS。
	//
	// S11 默认先用较小输出预算请求模型，只有遇到输出截断才升级。
	DefaultMaxTokens int64 = 8000

	// EscalatedMaxTokens 对标 Python ESCALATED_MAX_TOKENS。
	//
	// 第一次输出 token 达到上限时，不追加截断内容，直接用更高 max_tokens 重试同一请求。
	EscalatedMaxTokens int64 = 64000

	// MaxRecoveryRetries 对标 Python MAX_RECOVERY_RETRIES。
	//
	// 升级后仍然输出截断时，最多追加几次 continuation prompt。
	MaxRecoveryRetries = 3

	// MaxRetries 对标 Python MAX_RETRIES。
	//
	// 429 / 529 这类瞬时错误最多重试次数。
	MaxRetries = 10

	// BaseDelay 对标 Python BASE_DELAY_MS。
	//
	// 指数退避的初始等待时间。
	BaseDelay = 500 * time.Millisecond

	// MaxConsecutive529 对标 Python MAX_CONSECUTIVE_529。
	//
	// 连续 overloaded 到达阈值后尝试切 fallback model。
	MaxConsecutive529 = 3

	// ContinuationPrompt 对标 Python CONTINUATION_PROMPT。
	//
	// 升级到 64K 后仍然截断时，作为 user message 让模型继续输出。
	ContinuationPrompt = "Output token limit hit. Resume directly — no apology, no recap. Pick up mid-thought."
)

// State 对标 Python RecoveryState。
//
// 只记录 S11 错误恢复所需的状态：是否升级过 token、是否 compact 过、连续 529 次数和当前模型。

type State struct {
	HasEscalated                bool
	RecoveryCount               int
	Consecutive529              int
	HasAttemptedReactiveCompact bool
	CurrentModel                string
	FallbackModel               string
}

// NewState 对标 Python RecoveryState.__init__。
//
// 用 primary model 初始化本轮 agent loop 的恢复状态；fallback model 来自 FALLBACK_MODEL_ID。
func NewState(primaryModel string, fallbackModel string) State {
	return State{
		CurrentModel:  strings.TrimSpace(primaryModel),
		FallbackModel: strings.TrimSpace(fallbackModel),
	}
}

// WithRetry 对标 Python with_retry。
//
// 只包装 429 / 529 这类瞬时错误；非瞬时错误交给外层 agent loop 决定是否 reactive compact 或终止。
func WithRetry(
	ctx context.Context,
	state *State,
	fn func(model string) (*openai.ChatCompletion, error),
) (*openai.ChatCompletion, error) {
	if state == nil {
		s := NewState("", "")
		state = &s
	}

	for attempt := 0; attempt < MaxRetries; attempt++ {
		resp, err := fn(state.CurrentModel)
		if err == nil {
			state.Consecutive529 = 0
			return resp, nil
		}

		if IsRateLimit(err) {
			delay := RetryDelay(attempt, 0)
			fmt.Printf("  \033[33m[429 rate limit] retry %d/%d, wait %.1fs\033[0m\n",
				attempt+1,
				MaxRetries,
				delay.Seconds(),
			)

			if err := sleepContext(ctx, delay); err != nil {
				return nil, err
			}

			continue
		}

		if IsOverloaded(err) {
			state.Consecutive529++

			if state.Consecutive529 >= MaxConsecutive529 {
				if state.FallbackModel != "" {
					state.CurrentModel = state.FallbackModel
					state.Consecutive529 = 0

					fmt.Printf("  \033[31m[529 x%d] switching to %s\033[0m\n",
						MaxConsecutive529,
						state.FallbackModel,
					)
				} else {
					state.Consecutive529 = 0

					fmt.Printf("  \033[31m[529 x%d] no FALLBACK_MODEL_ID configured, continuing retry\033[0m\n",
						MaxConsecutive529,
					)
				}
			}

			delay := RetryDelay(attempt, 0)
			fmt.Printf("  \033[33m[529 overloaded] retry %d/%d, wait %.1fs\033[0m\n",
				attempt+1,
				MaxRetries,
				delay.Seconds(),
			)

			if err := sleepContext(ctx, delay); err != nil {
				return nil, err
			}

			continue
		}

		return nil, err
	}

	return nil, fmt.Errorf("max retries (%d) exceeded", MaxRetries)
}

// IsRateLimit 对标 Python with_retry 中的 429 判断。
//
// 用错误类型名和错误文本宽松识别 rate limit。
func IsRateLimit(err error) bool {
	if err == nil {
		return false
	}

	msg := strings.ToLower(err.Error())

	return strings.Contains(msg, "429") ||
		strings.Contains(msg, "rate limit") ||
		strings.Contains(msg, "ratelimit")
}

// IsOverloaded 对标 Python with_retry 中的 529 判断。
//
// 用错误类型名和错误文本宽松识别 overloaded。
func IsOverloaded(err error) bool {
	if err == nil {
		return false
	}

	msg := strings.ToLower(err.Error())

	return strings.Contains(msg, "529") ||
		strings.Contains(msg, "overloaded")
}

// RetryDelay 对标 Python retry_delay。
//
// 使用指数退避 + jitter；如果调用方传入 retryAfter，则优先使用 retryAfter。
func RetryDelay(attempt int, retryAfter time.Duration) time.Duration {
	if retryAfter > 0 {
		return retryAfter
	}

	base := BaseDelay * time.Duration(1<<attempt)
	max := 32 * time.Second
	if base > max {
		base = max
	}

	jitter := time.Duration(rand.Float64() * float64(base) * 0.25)

	return base + jitter
}
func sleepContext(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// ErrorText 对标 Python unrecoverable error assistant message。
//
// 把不可恢复错误变成简短文本，避免 panic 中断学习版 agent loop。
func ErrorText(err error) string {
	if err == nil {
		return "[Error] unknown error"
	}

	text := err.Error()
	if len([]rune(text)) > 200 {
		text = string([]rune(text)[:200])
	}

	return fmt.Sprintf("[Error] %T: %s", err, text)
}

// IsPromptTooLong 对标 Python is_prompt_too_long_error。
//
// 根据不同 OpenAI-compatible 服务的错误文案判断是否属于上下文过长。
func IsPromptTooLong(err error) bool {
	if err == nil {
		return false
	}

	msg := strings.ToLower(err.Error())

	return (strings.Contains(msg, "prompt") && strings.Contains(msg, "long")) ||
		strings.Contains(msg, "prompt_is_too_long") ||
		strings.Contains(msg, "prompt_too_long") ||
		strings.Contains(msg, "context_length_exceeded") ||
		strings.Contains(msg, "context length exceeded") ||
		strings.Contains(msg, "maximum context length") ||
		strings.Contains(msg, "max_context_window") ||
		strings.Contains(msg, "tokens exceed")
}

// IsMaxTokensFinishReason 对标 Python response.stop_reason == "max_tokens"。
//
// OpenAI Chat Completions 常见 finish_reason 是 length；兼容 max_tokens 字样。
func IsMaxTokensFinishReason(reason any) bool {
	s := strings.ToLower(strings.TrimSpace(fmt.Sprint(reason)))

	return s == "length" ||
		s == "max_tokens" ||
		strings.Contains(s, "max_tokens") ||
		strings.Contains(s, "length")
}
