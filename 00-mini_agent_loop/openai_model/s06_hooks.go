package openai_model

import (
	"AgentLoop/00-mini_agent_loop/openai_model/agentui"
	"AgentLoop/00-mini_agent_loop/openai_model/hooks"
	"context"
	"fmt"
	"strings"
)

func RegisterS06DefaultHooks(
	hookBus *hooks.HookBus,
	permission *PermissionChecker,
	workdir string,
) {
	RegisterS05DefaultHooks(hookBus, permission, workdir)
	RegisterS06SubagentHooks(hookBus)
}

func RegisterS06SubagentHooks(hookBus *hooks.HookBus) {
	hookBus.RegisterSubagentStart(SubagentStartOutputHook())
	hookBus.RegisterSubagentDone(SubagentDoneOutputHook())
	hookBus.RegisterSubagentError(SubagentErrorOutputHook())
}

func SubagentStartOutputHook() hooks.SubagentStartHook {
	return func(ctx context.Context, event hooks.SubagentStartContext) {
		line := agentui.FormatSubagentStart(ctx, event.ID, event.Description)
		if strings.TrimSpace(line) != "" {
			fmt.Println(line)
		}
	}
}

func SubagentDoneOutputHook() hooks.SubagentDoneHook {
	return func(ctx context.Context, event hooks.SubagentDoneContext) {
		line := agentui.FormatSubagentDone(ctx, event.ID, event.Summary, event.Elapsed)
		if strings.TrimSpace(line) != "" {
			fmt.Println(line)
		}
	}
}

func SubagentErrorOutputHook() hooks.SubagentErrorHook {
	return func(ctx context.Context, event hooks.SubagentErrorContext) {
		line := agentui.FormatSubagentError(ctx, event.ID, event.Err, event.Elapsed)
		if strings.TrimSpace(line) != "" {
			fmt.Println(line)
		}
	}
}
