package hookimpl

import (
	"AgentLoop/internal/hooks"
	"AgentLoop/internal/permission"
)

// register_hook("UserPromptSubmit", context_inject_hook)
// register_hook("PreToolUse", tool_call_output_hook)
// register_hook("PreToolUse", permission_hook)
// register_hook("PreToolUse", log_hook)
// register_hook("PostToolUse", large_output_hook)
// register_hook("Stop", summary_hook)
func RegisterS04DefaultHooks(
	hookBus *hooks.HookBus,
	checker *permission.PermissionChecker,
	workdir string,
) {
	hookBus.RegisterUserPromptSubmit(ContextInjectHook(workdir))
	hookBus.RegisterPreToolUse(ToolCallOutputHook())
	hookBus.RegisterPreToolUse(PermissionHook(checker))
	//hookBus.RegisterPreToolUse(LogHook())
	hookBus.RegisterPostToolUse(LargeOutputHook(100_000))
	hookBus.RegisterStop(SummaryHook())
}
