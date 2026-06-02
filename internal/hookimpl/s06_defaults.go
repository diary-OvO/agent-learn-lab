package hookimpl

import (
	"AgentLoop/internal/agentconsole"
	"AgentLoop/internal/hooks"
	"AgentLoop/internal/permission"
	"context"
	"fmt"
	"strings"
)

func RegisterS06DefaultHooks(
	hookBus *hooks.HookBus,
	checker *permission.PermissionChecker,
	workdir string,
) {
	RegisterS05DefaultHooks(hookBus, checker, workdir)
	RegisterS06SubagentHooks(hookBus)
}

func RegisterS06SubagentHooks(hookBus *hooks.HookBus) {
	hookBus.RegisterSubagentStart(SubagentStartOutputHook())
	hookBus.RegisterSubagentDone(SubagentDoneOutputHook())
	hookBus.RegisterSubagentError(SubagentErrorOutputHook())
}

func SubagentStartOutputHook() hooks.SubagentStartHook {
	return func(ctx context.Context, event hooks.SubagentStartContext) {
		line := agentconsole.FormatSubagentStart(ctx, event.ID, event.Description)
		if strings.TrimSpace(line) != "" {
			fmt.Println(line)
		}
	}
}

func SubagentDoneOutputHook() hooks.SubagentDoneHook {
	return func(ctx context.Context, event hooks.SubagentDoneContext) {
		line := agentconsole.FormatSubagentDone(ctx, event.ID, event.Summary, event.Elapsed)
		if strings.TrimSpace(line) != "" {
			fmt.Println(line)
		}
	}
}

func SubagentErrorOutputHook() hooks.SubagentErrorHook {
	return func(ctx context.Context, event hooks.SubagentErrorContext) {
		line := agentconsole.FormatSubagentError(ctx, event.ID, event.Err, event.Elapsed)
		if strings.TrimSpace(line) != "" {
			fmt.Println(line)
		}
	}
}
