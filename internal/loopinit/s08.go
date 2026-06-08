package loopinit

import (
	"AgentLoop/internal/hookimpl"
	"AgentLoop/internal/hooks"
	"AgentLoop/internal/permission"
	"AgentLoop/internal/skills"
	"AgentLoop/internal/subagent"
	"AgentLoop/internal/tools"

	v2 "AgentLoop/internal/toolkit/v2"
)

// InitS08SubToolbox 初始化 S08 子 Agent 的工具集
// 子 Agent 只有基础工具
func InitS08SubToolbox() *v2.ToolBox {
	return v2.NewToolBox(
		tools.NewWeatherToolV2(),
		tools.NewBashToolV2(),
		tools.NewReadFileToolV2(),
		tools.NewWriteFileToolV2(),
		tools.NewEditFileToolV2(),
		tools.NewGlobToolV2(),
	)
}

// InitS08Toolbox 初始化 S08 主 Agent 的工具集
// S08 对标 Python 原课的 context_compact 机制，工具与 S07 相同：
// Weather, Bash, ReadFile, WriteFile, EditFile, Glob, TodoWrite, Task, LoadSkill
func InitS08Toolbox(subAgent *subagent.SubAgent, skillRegistry *skills.Registry) *v2.ToolBox {
	return v2.NewToolBox(
		tools.NewWeatherToolV2(),
		tools.NewBashToolV2(),
		tools.NewReadFileToolV2(),
		tools.NewWriteFileToolV2(),
		tools.NewEditFileToolV2(),
		tools.NewGlobToolV2(),
		tools.NewTodoWriteToolV2(),
		tools.NewTaskToolV2(subAgent),
		tools.NewLoadSkillToolV2(skillRegistry),
	)
}

// InitS08Hooks 初始化 S08 的 hooks
// S08 继承 S06 的 hooks
func InitS08Hooks(hookBus *hooks.HookBus, checker *permission.PermissionChecker, workDir string) {
	hookimpl.RegisterS06DefaultHooks(hookBus, checker, workDir)
}
