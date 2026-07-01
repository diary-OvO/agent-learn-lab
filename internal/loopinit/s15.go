package loopinit

import (
	"AgentLoop/internal/cron"
	"AgentLoop/internal/hooks"
	"AgentLoop/internal/permission"
	"AgentLoop/internal/skills"
	"AgentLoop/internal/subagent"
	"AgentLoop/internal/tasks"
	"AgentLoop/internal/team"
	"AgentLoop/internal/tools"

	v2 "AgentLoop/internal/toolkit/v2"
)

// InitS15Hooks 对标 S15 继承 S14 hook。
//
// S15 新增 teammate 消息协作，但不新增 hook 类型或 hook 执行时机。
func InitS15Hooks(
	hookBus *hooks.HookBus,
	checker *permission.PermissionChecker,
	workDir string,
) {
	InitS14Hooks(hookBus, checker, workDir)
}

// InitS15SubToolbox 对标 S15 中既有 task subagent 仍沿用 S14。
//
// task 工具的同步 subagent 不参与 MessageBus，避免两种 agent 概念混在一起。
func InitS15SubToolbox() *v2.ToolBox {
	return InitS14SubToolbox()
}

// InitS15TeammateToolbox 对标 Python spawn_teammate_thread 里的 sub_tools。
//
// teammate 只拿 bash/read_file/write_file/send_message，保持教学版团队协作最小闭环。
func InitS15TeammateToolbox(
	messageBus *team.MessageBus,
	agentName string,
) *v2.ToolBox {
	return v2.NewToolBox(
		tools.NewBashV2ToolV2(),
		tools.NewReadFileToolV2(),
		tools.NewWriteFileToolV2(),
		tools.NewSendMessageToolV2(messageBus, agentName),
	)
}

// InitS15Toolbox 对标 Python S15 TOOLS。
//
// 在 S14 cron/background/task 工具基础上新增 spawn_teammate、send_message、check_inbox。
func InitS15Toolbox(
	subAgent *subagent.SubAgent,
	skillRegistry *skills.Registry,
	taskBoard tasks.Board,
	cronScheduler *cron.Scheduler,
	spawner *team.Spawner,
	messageBus *team.MessageBus,
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

		tools.NewSpawnTeammateToolV2(spawner),
		tools.NewSendMessageToolV2(messageBus, "lead"),
		tools.NewCheckInboxToolV2(messageBus, "lead"),
	)
}
