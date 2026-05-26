package hooks

import (
	"context"

	v2 "AgentLoop/00-mini_agent_loop/openai_model/tools/v2"
)

// hooks 是 S04 引入的 Harness 扩展点。
// 从这一章开始，用户输入、工具执行前后、循环停止等过程事件可以注册到 HookBus 中，
// 让这些额外逻辑独立于 Agent Loop 本身，避免把审计、提示、阻断等逻辑散落在循环代码里。

// UserPromptSubmitHook 在用户提交 prompt 后触发。
// 返回非空字符串表示 hook 已经生成需要处理的结果，后续同类 hook 不再继续执行。
type UserPromptSubmitHook func(ctx context.Context, query string) string

// PreToolUseHook 在工具执行前触发，适合做权限检查、日志记录或执行前提示。
// 返回非空字符串表示当前工具调用被 hook 接管，后续同类 hook 不再继续执行。
type PreToolUseHook func(ctx context.Context, call v2.ToolCall) string

// PostToolUseHook 在工具执行后触发，适合做结果审计、输出摘要或执行后提醒。
// 返回非空字符串表示 hook 产生了额外结果，后续同类 hook 不再继续执行。
type PostToolUseHook func(ctx context.Context, call v2.ToolCall, output string) string

// StopHook 在 Agent Loop 准备停止时触发，适合做停止条件检查或最终状态汇总。
// 返回非空字符串表示 hook 生成了停止阶段的处理结果，后续同类 hook 不再继续执行。
type StopHook func(ctx context.Context, stop StopContext) string

// StopContext 描述 Agent Loop 停止时的上下文信息。
type StopContext struct {
	// MessageCount 是当前对话中累计的消息数量。
	MessageCount int

	// ToolCallCount 是当前任务中累计执行过的工具调用数量。
	ToolCallCount int
}

// HookBus 保存不同阶段的 hook 列表，并负责按注册顺序触发它们。
type HookBus struct {
	userPromptSubmit []UserPromptSubmitHook
	preToolUse       []PreToolUseHook
	postToolUse      []PostToolUseHook
	stop             []StopHook
}

// NewHookBus 创建一个空的 HookBus。
func NewHookBus() *HookBus {
	return &HookBus{
		userPromptSubmit: make([]UserPromptSubmitHook, 0),
		preToolUse:       make([]PreToolUseHook, 0),
		postToolUse:      make([]PostToolUseHook, 0),
		stop:             make([]StopHook, 0),
	}
}

// RegisterUserPromptSubmit 注册用户提交 prompt 后触发的 hook。
func (h *HookBus) RegisterUserPromptSubmit(fn UserPromptSubmitHook) {
	h.userPromptSubmit = append(h.userPromptSubmit, fn)
}

// RegisterPreToolUse 注册工具执行前触发的 hook。
func (h *HookBus) RegisterPreToolUse(fn PreToolUseHook) {
	h.preToolUse = append(h.preToolUse, fn)
}

// RegisterPostToolUse 注册工具执行后触发的 hook。
func (h *HookBus) RegisterPostToolUse(fn PostToolUseHook) {
	h.postToolUse = append(h.postToolUse, fn)
}

// RegisterStop 注册 Agent Loop 停止时触发的 hook。
func (h *HookBus) RegisterStop(fn StopHook) {
	h.stop = append(h.stop, fn)
}

// TriggerUserPromptSubmit 按注册顺序触发用户提交 prompt 的 hook。
// 如果某个 hook 返回非空字符串，则立即返回该结果。
func (h *HookBus) TriggerUserPromptSubmit(ctx context.Context, query string) string {
	for _, fn := range h.userPromptSubmit {
		if result := fn(ctx, query); result != "" {
			return result
		}
	}
	return ""
}

// TriggerPreToolUse 按注册顺序触发工具执行前的 hook。
// 如果某个 hook 返回非空字符串，则立即返回该结果。
func (h *HookBus) TriggerPreToolUse(ctx context.Context, call v2.ToolCall) string {
	for _, fn := range h.preToolUse {
		if result := fn(ctx, call); result != "" {
			return result
		}
	}
	return ""
}

// TriggerPostToolUse 按注册顺序触发工具执行后的 hook。
// 如果某个 hook 返回非空字符串，则立即返回该结果。
func (h *HookBus) TriggerPostToolUse(ctx context.Context, call v2.ToolCall, output string) string {
	for _, fn := range h.postToolUse {
		if result := fn(ctx, call, output); result != "" {
			return result
		}
	}
	return ""
}

// TriggerStop 按注册顺序触发 Agent Loop 停止阶段的 hook。
// 如果某个 hook 返回非空字符串，则立即返回该结果。
func (h *HookBus) TriggerStop(ctx context.Context, stop StopContext) string {
	for _, fn := range h.stop {
		if result := fn(ctx, stop); result != "" {
			return result
		}
	}
	return ""
}
