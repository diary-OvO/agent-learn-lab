package loopinit

import (
	"AgentLoop/internal/cron"
	"AgentLoop/internal/hooks"
	"AgentLoop/internal/permission"
	"AgentLoop/internal/skills"
	"AgentLoop/internal/subagent"
	"AgentLoop/internal/tasks"
	"AgentLoop/internal/tools"

	v2 "AgentLoop/internal/toolkit/v2"
)

// InitS14Hooks 对标 S14 继承 S13 hook。
//
// S14 新增 cron 调度器，但不新增 hook 类型或 hook 执行时机。
func InitS14Hooks(
	hookBus *hooks.HookBus,
	checker *permission.PermissionChecker,
	workDir string,
) {
	InitS13Hooks(hookBus, checker, workDir)
}

// InitS14SubToolbox 对标 S14 cron 只唤醒主 Agent。
//
// 子 Agent 继续使用 S13 同步工具箱，避免 cron 生命周期扩散到子 Agent。
func InitS14SubToolbox() *v2.ToolBox {
	return InitS13SubToolbox()
}

// InitS14Toolbox 对标 Python S14 TOOLS。
//
// 在 S13 后台任务工具基础上新增 schedule_cron、list_crons、cancel_cron。
func InitS14Toolbox(
	subAgent *subagent.SubAgent,
	skillRegistry *skills.Registry,
	taskBoard tasks.Board,
	cronScheduler *cron.Scheduler,
) *v2.ToolBox {
	return v2.NewToolBox(
		tools.NewWeatherToolV2(),

		tools.NewBashV2ToolV2WithBackground(),

		tools.NewReadFileToolV2(),
		tools.NewWriteFileToolV2(),
		tools.NewEditFileToolV2(),
		tools.NewGlobToolV2(),

		tools.NewTaskToolV2(subAgent),
		tools.NewLoadSkillToolV2(skillRegistry),

		tools.NewCreateTaskToolV2(taskBoard),
		tools.NewListTasksToolV2(taskBoard),
		tools.NewGetTaskToolV2(taskBoard),
		tools.NewClaimTaskToolV2(taskBoard),
		tools.NewCompleteTaskToolV2(taskBoard),

		tools.NewScheduleCronToolV2(cronScheduler),
		tools.NewListCronsToolV2(cronScheduler),
		tools.NewCancelCronToolV2(cronScheduler),
	)
}
