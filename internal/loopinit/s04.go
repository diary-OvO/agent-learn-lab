package loopinit

import (
	"AgentLoop/internal/hookimpl"
	"AgentLoop/internal/hooks"
	"AgentLoop/internal/permission"
	"AgentLoop/internal/tools"

	v2 "AgentLoop/internal/toolkit/v2"
)

// InitS04Toolbox 初始化 S04 的工具集
// S04 对标 Python 原课的 hooks 机制，工具与 S03 相同：
// Weather, Bash, ReadFile, WriteFile, EditFile
func InitS04Toolbox() *v2.ToolBox {
	return v2.NewToolBox(
		tools.NewWeatherToolV2(),
		tools.NewBashToolV2(),
		tools.NewReadFileToolV2(),
		tools.NewWriteFileToolV2(),
		tools.NewEditFileToolV2(),
	)
}

// InitS04Hooks 初始化 S04 的 hooks
// S04 引入 HookBus，注册 permission、logging 等 hooks
func InitS04Hooks(hookBus *hooks.HookBus, checker *permission.PermissionChecker, workDir string) {
	hookimpl.RegisterS04DefaultHooks(hookBus, checker, workDir)
}
