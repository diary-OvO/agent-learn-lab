package loopinit

import (
	"AgentLoop/internal/hooks"
	"AgentLoop/internal/permission"
	"AgentLoop/internal/skills"
	"AgentLoop/internal/subagent"
	"AgentLoop/internal/tasks"
	"AgentLoop/internal/tools"

	v2 "AgentLoop/internal/toolkit/v2"
)

// InitS13Hooks 对标 S13 继承 S12 hook。
//
// S13 只改变工具执行策略，不增加新的 hook 类型。
func InitS13Hooks(
	hookBus *hooks.HookBus,
	checker *permission.PermissionChecker,
	workDir string,
) {
	InitS12Hooks(hookBus, checker, workDir)
}

// InitS13SubToolbox 对标 S13 后台执行只用于主 Agent bash。
//
// 子 Agent 继续使用原有同步工具箱，避免把后台生命周期扩散到子 Agent。
func InitS13SubToolbox() *v2.ToolBox {
	return InitS12SubToolbox()
}

// InitS13Toolbox 对标 Python S13 TOOLS。
//
// 相比 S12 只把 bash 替换为支持 run_in_background 参数的版本；
// 任务图工具数量和语义保持不变。
func InitS13Toolbox(
	subAgent *subagent.SubAgent,
	skillRegistry *skills.Registry,
	taskStore tasks.Store,
) *v2.ToolBox {
	return v2.NewToolBox(
		tools.NewWeatherToolV2(),

		tools.NewBashV2ToolV2WithBackground(),

		tools.NewReadFileToolV2(),
		tools.NewWriteFileToolV2(),
		tools.NewEditFileToolV2(),
		tools.NewGlobToolV2(),

		// task 是即时启动子智能体，不是持久化任务记录。
		tools.NewTaskToolV2(subAgent),
		tools.NewLoadSkillToolV2(skillRegistry),

		// S12 持久化任务图。
		tools.NewCreateTaskToolV2(taskStore),
		tools.NewListTasksToolV2(taskStore),
		tools.NewGetTaskToolV2(taskStore),
		tools.NewClaimTaskToolV2(taskStore),
		tools.NewCompleteTaskToolV2(taskStore),
	)
}
