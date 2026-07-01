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

// InitS17Hooks 对标 S17 继承 S16 hooks。
//
// 迭代原因：S17 新增 autonomous teammate lifecycle，但生命周期由 Spawner
// 和任务板驱动，不需要扩展 hook bus 的事件类型。
//
// 与旧函数差别：InitS16Hooks 已经包含 S15/S16 所需 hook；这里保留新的
// S17 入口函数，是为了让课程 main.go 明确表达“当前跑的是 S17”，同时复用旧 hook 行为。
func InitS17Hooks(
	hookBus *hooks.HookBus,
	checker *permission.PermissionChecker,
	workDir string,
) {
	InitS16Hooks(hookBus, checker, workDir)
}

// InitS17SubToolbox 对标 S17 常规 task subagent 仍沿用 S16。
//
// 迭代原因：S17 只有 teammate 需要空闲自驱和任务板认领能力，普通 task subagent
// 仍是一次性委派模型，不应该被悄悄升级成 autonomous agent。
//
// 与旧函数差别：InitS16SubToolbox 继续提供常规子智能体工具箱；S17 新增的
// InitS17TeammateToolbox 专门给 team.Spawner 创建的 teammate 使用。
func InitS17SubToolbox() *v2.ToolBox {
	return InitS16SubToolbox()
}

// InitS17TeammateToolbox 对标 Python S17 teammate sub_tools。
//
// 迭代原因：S15/S16 teammate 主要处理 Lead 分配的 prompt 或协议消息；
// S17 teammate 空闲时要自己查看任务板、认领任务并完成任务。
//
// 与旧函数差别：InitS16TeammateToolbox 只包含 bash/read/write/send/submit_plan
// 这类协作工具；这里额外加入 list_tasks、owner-aware claim_task、complete_task，
// 并把 claim_task 绑定到当前 agentName，保证任务 owner 是具体 teammate。
func InitS17TeammateToolbox(
	messageBus *team.MessageBus,
	protocolBook *team.ProtocolBook,
	taskBoard tasks.Board,
	agentName string,
) *v2.ToolBox {
	return v2.NewToolBox(
		tools.NewBashV2ToolV2(),
		tools.NewReadFileToolV2(),
		tools.NewWriteFileToolV2(),

		tools.NewSendMessageToolV2(messageBus, agentName),
		tools.NewSubmitPlanToolV2(messageBus, protocolBook, agentName),

		tools.NewListTasksToolV2(taskBoard),
		tools.NewClaimTaskToolV2WithOwner(taskBoard, agentName),
		tools.NewCompleteTaskToolV2(taskBoard),
	)
}

// InitS17Toolbox 对标 Python S17 Lead TOOLS。
//
// 迭代原因：Lead 侧仍然需要 S16 的 protocol 工具，但 spawn_teammate 的语义升级为
// 创建 autonomous teammate；同时 Lead 自己 claim_task 时也应通过 owner-aware 路径写入 owner。
//
// 与旧函数差别：InitS16Toolbox 使用 persistent teammate spawner，teammate 等待 Lead
// 通过 inbox/protocol 驱动；InitS17Toolbox 保留 S16 工具集合，但把 spawn_teammate
// 换成 NewSpawnAutonomousTeammateToolV2，并把 Lead claim_task 换成带 owner 的版本。
func InitS17Toolbox(
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
		tools.NewClaimTaskToolV2WithOwner(taskBoard, "agent"),
		tools.NewCompleteTaskToolV2(taskBoard),

		tools.NewScheduleCronToolV2(cronScheduler),
		tools.NewListCronsToolV2(cronScheduler),
		tools.NewCancelCronToolV2(cronScheduler),

		tools.NewSpawnAutonomousTeammateToolV2(spawner),
		tools.NewSendMessageToolV2(messageBus, "lead"),
		tools.NewCheckInboxToolV2(messageBus, protocolBook),

		tools.NewRequestShutdownToolV2(messageBus, protocolBook),
		tools.NewRequestPlanToolV2(messageBus, protocolBook),
		tools.NewReviewPlanToolV2(messageBus, protocolBook),
	)
}
