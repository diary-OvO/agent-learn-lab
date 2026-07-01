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

// InitS16Hooks 对标 S16 继承 S15 hooks。
//
// S16 新增 team protocol，不新增 hook 类型。
func InitS16Hooks(
	hookBus *hooks.HookBus,
	checker *permission.PermissionChecker,
	workDir string,
) {
	InitS15Hooks(hookBus, checker, workDir)
}

// InitS16SubToolbox 对标 S16 常规 subagent 仍沿用 S15。
//
// task 工具启动的是同步子 Agent；team teammate 的协议工具由 InitS16TeammateToolbox 单独组装。
func InitS16SubToolbox() *v2.ToolBox {
	return InitS15SubToolbox()
}

// InitS16TeammateToolbox 对标 Python S16 teammate sub_tools。
//
// 相比 S15 teammate，新增 submit_plan，用于向 Lead 发起 plan_approval_request。
func InitS16TeammateToolbox(
	messageBus *team.MessageBus,
	protocolBook *team.ProtocolBook,
	agentName string,
) *v2.ToolBox {
	return v2.NewToolBox(
		tools.NewBashV2ToolV2(),
		tools.NewReadFileToolV2(),
		tools.NewWriteFileToolV2(),
		tools.NewSendMessageToolV2(messageBus, agentName),
		tools.NewSubmitPlanToolV2(messageBus, protocolBook, agentName),
	)
}

// InitS16Toolbox 对标 Python S16 TOOLS。
//
// 在 S15 工具基础上切换为 persistent teammate，并新增 request_shutdown、request_plan、review_plan。
func InitS16Toolbox(
	subAgent *subagent.SubAgent,
	skillRegistry *skills.Registry,
	taskBoard tasks.Board,
	cronScheduler *cron.Scheduler,
	spawner *team.Spawner,
	messageBus *team.MessageBus,
	protocolBook *team.ProtocolBook,
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

		tools.NewSpawnPersistentTeammateToolV2(spawner),
		tools.NewSendMessageToolV2(messageBus, "lead"),
		tools.NewCheckInboxToolV2(messageBus, protocolBook),

		// S16 新增：Lead protocol tools。
		tools.NewRequestShutdownToolV2(messageBus, protocolBook),
		tools.NewRequestPlanToolV2(messageBus, protocolBook),
		tools.NewReviewPlanToolV2(messageBus, protocolBook),
	)
}
