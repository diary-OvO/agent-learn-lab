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

// InitS07SubToolbox 初始化 S07 子 Agent 的工具集
// 子 Agent 只有基础工具
func InitS07SubToolbox() *v2.ToolBox {
	return v2.NewToolBox(
		tools.NewWeatherToolV2(),
		tools.NewBashToolV2(),
		tools.NewReadFileToolV2(),
		tools.NewWriteFileToolV2(),
		tools.NewEditFileToolV2(),
		tools.NewGlobToolV2(),
	)
}

// InitS07Toolbox 初始化 S07 主 Agent 的工具集
// S07 对标 Python 原课的 skill_loading 机制，新增 LoadSkillTool：
// Weather, Bash, ReadFile, WriteFile, EditFile, Glob, TodoWrite, Task, LoadSkill
func InitS07Toolbox(subAgent *subagent.SubAgent, skillRegistry *skills.Registry) *v2.ToolBox {
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

// InitS07Hooks 初始化 S07 的 hooks
// S07 继承 S06 的 hooks
func InitS07Hooks(hookBus *hooks.HookBus, checker *permission.PermissionChecker, workDir string) {
	hookimpl.RegisterS06DefaultHooks(hookBus, checker, workDir)
}
