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

// InitS12Hooks 对标 S12 继承已有 hook。
//
// S12 新增任务系统，不新增 hook 类型或 hook 执行时机。
func InitS12Hooks(
	hookBus *hooks.HookBus,
	checker *permission.PermissionChecker,
	workDir string,
) {
	InitS11Hooks(hookBus, checker, workDir)
}

// InitS12SubToolbox 对标 S12 主任务系统只属于主 Agent。
//
// 子 Agent 继续沿用 S11 工具箱，不直接修改主 Agent 的持久任务图。
func InitS12SubToolbox() *v2.ToolBox {
	return InitS11SubToolbox()
}

// InitS12Toolbox 对标 Python S12 TOOLS。
//
// 在 S11 已有工具基础上新增 create/list/get/claim/complete 五个持久任务工具。
func InitS12Toolbox(
	subAgent *subagent.SubAgent,
	skillRegistry *skills.Registry,
	taskStore tasks.Store,
) *v2.ToolBox {
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

		tools.NewCreateTaskToolV2(taskStore),
		tools.NewListTasksToolV2(taskStore),
		tools.NewGetTaskToolV2(taskStore),
		tools.NewClaimTaskToolV2(taskStore),
		tools.NewCompleteTaskToolV2(taskStore),
	)
}
