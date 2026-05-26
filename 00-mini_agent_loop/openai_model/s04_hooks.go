package openai_model

import (
	"AgentLoop/00-mini_agent_loop/openai_model/hooks"
	v2 "AgentLoop/00-mini_agent_loop/openai_model/tools/v2"
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// RegisterS04DefaultHooks 注册 S04 课程里的默认 hooks。
//
// 对应 Python:
//
// register_hook("UserPromptSubmit", context_inject_hook)
// register_hook("PreToolUse", permission_hook)
// register_hook("PreToolUse", log_hook)
// register_hook("PostToolUse", large_output_hook)
// register_hook("Stop", summary_hook)
func RegisterS04DefaultHooks(
	hookBus *hooks.HookBus,
	permission *PermissionChecker,
	workdir string,
) {
	hookBus.RegisterUserPromptSubmit(ContextInjectHook(workdir))
	hookBus.RegisterPreToolUse(PermissionHook(permission))
	hookBus.RegisterPreToolUse(LogHook())
	hookBus.RegisterPostToolUse(LargeOutputHook(100_000))
	hookBus.RegisterStop(SummaryHook())
}

// PermissionHook 把 S03 的 PermissionChecker 包装成 PreToolUse hook。
// 返回非空字符串表示阻止本次工具调用。
func PermissionHook(permission *PermissionChecker) hooks.PreToolUseHook {
	return func(ctx context.Context, call v2.ToolCall) string {
		if permission == nil {
			return ""
		}

		if !permission.CheckPermission(ctx, call) {
			return "Permission denied by hook."
		}

		return ""
	}
}

// LogHook 记录每次工具调用。
// 它只观察，不阻断。
func LogHook() hooks.PreToolUseHook {
	return func(_ context.Context, call v2.ToolCall) string {
		fmt.Printf(
			"\033[90m[HOOK] %s(%s)\033[0m\n",
			call.Name,
			argsPreview(call.Arguments, 80),
		)

		return ""
	}
}

// LargeOutputHook 在工具执行后检查输出是否过大。
// 它只提醒，不修改结果。
func LargeOutputHook(threshold int) hooks.PostToolUseHook {
	return func(_ context.Context, call v2.ToolCall, output string) string {
		if threshold <= 0 {
			threshold = 100_000
		}

		if len([]rune(output)) > threshold {
			fmt.Printf(
				"\033[33m[HOOK] ⚠ Large output from %s: %d chars\033[0m\n",
				call.Name,
				len([]rune(output)),
			)
		}

		return ""
	}
}

// ContextInjectHook 在用户输入提交前触发。
// 教学版只打印当前工作区，不修改消息。
func ContextInjectHook(workdir string) hooks.UserPromptSubmitHook {
	return func(_ context.Context, _ string) string {
		fmt.Printf(
			"\033[90m[HOOK] UserPromptSubmit: working in %s\033[0m\n",
			workdir,
		)

		return ""
	}
}

// SummaryHook 在 Agent Loop 即将结束时触发。
// 教学版只打印工具调用统计。
func SummaryHook() hooks.StopHook {
	return func(_ context.Context, stop hooks.StopContext) string {
		fmt.Printf(
			"\033[90m[HOOK] Stop: session used %d tool calls, %d messages\033[0m\n",
			stop.ToolCallCount,
			stop.MessageCount,
		)

		return ""
	}
}

func argsPreview(raw json.RawMessage, limit int) string {
	text := strings.TrimSpace(string(raw))

	var decoded any
	if err := json.Unmarshal(raw, &decoded); err == nil {
		if b, err := json.Marshal(decoded); err == nil {
			text = string(b)
		}
	}

	return truncateRunes(text, limit)
}

func truncateRunes(s string, limit int) string {
	if limit <= 0 {
		return s
	}

	runes := []rune(s)
	if len(runes) <= limit {
		return s
	}

	return string(runes[:limit]) + "...<truncated>"
}
