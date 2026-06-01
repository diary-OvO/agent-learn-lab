package hooks

import (
	"context"
	"time"

	v2 "AgentLoop/00-mini_agent_loop/openai_model/tools/v2"
)

type UserPromptSubmitHook func(ctx context.Context, query string) string
type PreToolUseHook func(ctx context.Context, call v2.ToolCall) string
type PostToolUseHook func(ctx context.Context, call v2.ToolCall, output string) string
type StopHook func(ctx context.Context, stop StopContext) string

type SubagentStartHook func(ctx context.Context, event SubagentStartContext)
type SubagentDoneHook func(ctx context.Context, event SubagentDoneContext)
type SubagentErrorHook func(ctx context.Context, event SubagentErrorContext)

type StopContext struct {
	MessageCount  int
	ToolCallCount int
}

type SubagentStartContext struct {
	ID          string
	Description string
}

type SubagentDoneContext struct {
	ID      string
	Summary string
	Elapsed time.Duration
}

type SubagentErrorContext struct {
	ID      string
	Err     error
	Elapsed time.Duration
}

type HookBus struct {
	userPromptSubmit []UserPromptSubmitHook
	preToolUse       []PreToolUseHook
	postToolUse      []PostToolUseHook
	stop             []StopHook

	subagentStart []SubagentStartHook
	subagentDone  []SubagentDoneHook
	subagentError []SubagentErrorHook
}

func NewHookBus() *HookBus {
	return &HookBus{
		userPromptSubmit: make([]UserPromptSubmitHook, 0),
		preToolUse:       make([]PreToolUseHook, 0),
		postToolUse:      make([]PostToolUseHook, 0),
		stop:             make([]StopHook, 0),
		subagentStart:    make([]SubagentStartHook, 0),
		subagentDone:     make([]SubagentDoneHook, 0),
		subagentError:    make([]SubagentErrorHook, 0),
	}
}

func (h *HookBus) RegisterUserPromptSubmit(fn UserPromptSubmitHook) {
	h.userPromptSubmit = append(h.userPromptSubmit, fn)
}

func (h *HookBus) RegisterPreToolUse(fn PreToolUseHook) {
	h.preToolUse = append(h.preToolUse, fn)
}

func (h *HookBus) RegisterPostToolUse(fn PostToolUseHook) {
	h.postToolUse = append(h.postToolUse, fn)
}

func (h *HookBus) RegisterStop(fn StopHook) {
	h.stop = append(h.stop, fn)
}

func (h *HookBus) RegisterSubagentStart(fn SubagentStartHook) {
	h.subagentStart = append(h.subagentStart, fn)
}

func (h *HookBus) RegisterSubagentDone(fn SubagentDoneHook) {
	h.subagentDone = append(h.subagentDone, fn)
}

func (h *HookBus) RegisterSubagentError(fn SubagentErrorHook) {
	h.subagentError = append(h.subagentError, fn)
}

func (h *HookBus) TriggerUserPromptSubmit(ctx context.Context, query string) string {
	if h == nil {
		return query
	}

	next := query
	for _, fn := range h.userPromptSubmit {
		if result := fn(ctx, next); result != "" {
			next = result
			break
		}
	}

	return next
}

func (h *HookBus) TriggerPreToolUse(ctx context.Context, call v2.ToolCall) string {
	if h == nil {
		return ""
	}

	for _, fn := range h.preToolUse {
		if result := fn(ctx, call); result != "" {
			return result
		}
	}

	return ""
}

func (h *HookBus) TriggerPostToolUse(ctx context.Context, call v2.ToolCall, output string) string {
	if h == nil {
		return output
	}

	next := output
	for _, fn := range h.postToolUse {
		if result := fn(ctx, call, next); result != "" {
			next = result
		}
	}

	return next
}

func (h *HookBus) TriggerStop(ctx context.Context, stop StopContext) string {
	if h == nil {
		return ""
	}

	for _, fn := range h.stop {
		if result := fn(ctx, stop); result != "" {
			return result
		}
	}

	return ""
}

func (h *HookBus) TriggerSubagentStart(ctx context.Context, event SubagentStartContext) {
	if h == nil {
		return
	}

	for _, fn := range h.subagentStart {
		fn(ctx, event)
	}
}

func (h *HookBus) TriggerSubagentDone(ctx context.Context, event SubagentDoneContext) {
	if h == nil {
		return
	}

	for _, fn := range h.subagentDone {
		fn(ctx, event)
	}
}

func (h *HookBus) TriggerSubagentError(ctx context.Context, event SubagentErrorContext) {
	if h == nil {
		return
	}

	for _, fn := range h.subagentError {
		fn(ctx, event)
	}
}
