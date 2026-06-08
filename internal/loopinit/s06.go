package loopinit

import (
	"AgentLoop/internal/hookimpl"
	"AgentLoop/internal/hooks"
	"AgentLoop/internal/permission"
	"AgentLoop/internal/subagent"
	"AgentLoop/internal/tools"

	v2 "AgentLoop/internal/toolkit/v2"
)

// InitS06SubToolbox 初始化 S06 子 Agent 的工具集
// 子 Agent 只有基础工具，不包括 TodoWrite 和 Task
func InitS06SubToolbox() *v2.ToolBox {
	return v2.NewToolBox(
		tools.NewWeatherToolV2(),
		tools.NewBashToolV2(),
		tools.NewReadFileToolV2(),
		tools.NewWriteFileToolV2(),
		tools.NewEditFileToolV2(),
		tools.NewGlobToolV2(),
	)
}

// InitS06Toolbox 初始化 S06 主 Agent 的工具集
// S06 对标 Python 原课的 subagent 机制，新增 TaskTool：
// Weather, Bash, ReadFile, WriteFile, EditFile, Glob, TodoWrite, Task
func InitS06Toolbox(subAgent *subagent.SubAgent) *v2.ToolBox {
	return v2.NewToolBox(
		tools.NewWeatherToolV2(),
		tools.NewBashToolV2(),
		tools.NewReadFileToolV2(),
		tools.NewWriteFileToolV2(),
		tools.NewEditFileToolV2(),
		tools.NewGlobToolV2(),
		tools.NewTodoWriteToolV2(),
		tools.NewTaskToolV2(subAgent),
	)
}

// InitS06Hooks 初始化 S06 的 hooks
// S06 继承 S04/S05 的 hooks
func InitS06Hooks(hookBus *hooks.HookBus, checker *permission.PermissionChecker, workDir string) {
	hookimpl.RegisterS06DefaultHooks(hookBus, checker, workDir)
}
