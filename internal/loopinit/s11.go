package loopinit

import (
	"AgentLoop/internal/hooks"
	"AgentLoop/internal/permission"
	"AgentLoop/internal/skills"
	"AgentLoop/internal/subagent"
	v2 "AgentLoop/internal/toolkit/v2"
)

func InitS11Hooks(hookBus *hooks.HookBus, checker *permission.PermissionChecker, workDir string) {
	InitS08Hooks(hookBus, checker, workDir)
}

func InitS11SubToolbox() *v2.ToolBox {
	return InitS08SubToolbox()
}

func InitS11Toolbox(subAgent *subagent.SubAgent, skillRegistry *skills.Registry) *v2.ToolBox {
	return InitS08Toolbox(subAgent, skillRegistry)
}
