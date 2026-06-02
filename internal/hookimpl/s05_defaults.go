package hookimpl

import (
	"AgentLoop/internal/hooks"
	"AgentLoop/internal/permission"
)

func RegisterS05DefaultHooks(
	hookBus *hooks.HookBus,
	checker *permission.PermissionChecker,
	workdir string,
) {
	hookBus.RegisterUserPromptSubmit(ContextInjectHook(workdir))
	hookBus.RegisterPreToolUse(ToolCallOutputHook())
	hookBus.RegisterPreToolUse(PermissionHook(checker))
	hookBus.RegisterPreToolUse(LogHook())
	hookBus.RegisterPostToolUse(LargeOutputHook(100_000))
	hookBus.RegisterPostToolUse(ToolResultOutputHook())
	hookBus.RegisterStop(SummaryHook())
}
