package openai_model

import (
	"AgentLoop/00-mini_agent_loop/openai_model/hooks"
)

func RegisterS05DefaultHooks(
	hookBus *hooks.HookBus,
	permission *PermissionChecker,
	workdir string,
) {
	hookBus.RegisterUserPromptSubmit(ContextInjectHook(workdir))
	hookBus.RegisterPreToolUse(ToolCallOutputHook())
	hookBus.RegisterPreToolUse(PermissionHook(permission))
	hookBus.RegisterPreToolUse(LogHook())
	hookBus.RegisterPostToolUse(LargeOutputHook(100_000))
	hookBus.RegisterPostToolUse(ToolResultOutputHook())
	hookBus.RegisterStop(SummaryHook())
}
