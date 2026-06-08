package loopinit

import (
	"AgentLoop/internal/hookimpl"
	"AgentLoop/internal/hooks"
	"AgentLoop/internal/permission"
	"AgentLoop/internal/tools"

	v2 "AgentLoop/internal/toolkit/v2"
)

// InitS05Toolbox 初始化 S05 的工具集
// S05 对标 Python 原课的 todo_write 机制，新增 TodoWriteTool 和 GlobTool：
// Weather, Bash, ReadFile, WriteFile, EditFile, Glob, TodoWrite
func InitS05Toolbox() *v2.ToolBox {
	return v2.NewToolBox(
		tools.NewWeatherToolV2(),
		tools.NewBashToolV2(),
		tools.NewReadFileToolV2(),
		tools.NewWriteFileToolV2(),
		tools.NewEditFileToolV2(),
		tools.NewGlobToolV2(),
		tools.NewTodoWriteToolV2(),
	)
}

// InitS05Hooks 初始化 S05 的 hooks
// S05 继承 S04 的 hooks
func InitS05Hooks(hookBus *hooks.HookBus, checker *permission.PermissionChecker, workDir string) {
	hookimpl.RegisterS05DefaultHooks(hookBus, checker, workDir)
}
