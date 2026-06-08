package loopinit

import (
	"AgentLoop/internal/tools"

	v2 "AgentLoop/internal/toolkit/v2"
)

// InitS03Toolbox 初始化 S03 的工具集
// S03 对标 Python 原课的 permission 机制，工具包括：
// Weather, Bash, ReadFile, WriteFile, EditFile
func InitS03Toolbox() *v2.ToolBox {
	return v2.NewToolBox(
		tools.NewWeatherToolV2(),
		tools.NewBashToolV2(),
		tools.NewReadFileToolV2(),
		tools.NewWriteFileToolV2(),
		tools.NewEditFileToolV2(),
	)
}

// InitS03Hooks 初始化 S03 的 hooks
// S03 没有使用 HookBus，而是直接在 runAgentLoop 中实现 permission 检查
// 这里保持空实现，表示 S03 的 hook 机制还在 main.go 中展示
func InitS03Hooks() {
	// S03 的 permission 检查逻辑直接在 main.go 的 runAgentLoop 中
	// 不使用 HookBus，所以这里不需要初始化
}
