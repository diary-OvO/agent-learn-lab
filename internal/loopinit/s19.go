package loopinit

import (
	"AgentLoop/internal/cron"
	"AgentLoop/internal/hooks"
	"AgentLoop/internal/mcp"
	"AgentLoop/internal/permission"
	"AgentLoop/internal/skills"
	"AgentLoop/internal/subagent"
	"AgentLoop/internal/tasks"
	"AgentLoop/internal/team"
	"AgentLoop/internal/tools"
	"AgentLoop/internal/worktree"

	v2 "AgentLoop/internal/toolkit/v2"
)

// InitS19Hooks 对标 S19 继承 S18 hooks。
//
// 迭代原因：S19 新增 MCP 工具发现，但不新增 hook 类型；MCP 动态工具仍通过
// 原有 PreToolUse/PostToolUse 路径执行。
//
// 与 InitS18Hooks 差别：这里只提供 S19 课程入口名，实际注册逻辑继续复用 S18。
func InitS19Hooks(
	hookBus *hooks.HookBus,
	checker *permission.PermissionChecker,
	workDir string,
) {
	InitS18Hooks(hookBus, checker, workDir)
}

// InitS19SubToolbox 对标 S19 常规 task subagent。
//
// 迭代原因：官方 S19 的 MCP 增量只作用于 Lead 动态 tool pool；
// 普通 task subagent 继续沿用 S18 前序能力，避免把 MCP 机制过早扩散到所有子 Agent。
//
// 与 InitS18SubToolbox 差别：行为完全复用，保留 S19 命名用于课程 main.go 对照。
func InitS19SubToolbox() *v2.ToolBox {
	return InitS18SubToolbox()
}

// InitS19TeammateToolbox 对标 Python S19 teammate sub_tools。
//
// 迭代原因：S19 Lead 新增 MCP 动态工具，但 teammate 仍是 S18 的 worktree-aware
// autonomous teammate，不参与 MCP discovery。
//
// 与 InitS18TeammateToolbox 差别：行为完全复用，显式保留旧函数给 S18 使用。
func InitS19TeammateToolbox(
	messageBus *team.MessageBus,
	protocolBook *team.ProtocolBook,
	taskBoard tasks.Board,
	agentName string,
	cwdProvider func() string,
	afterClaim func(tasks.Task),
	afterComplete func(tasks.Task),
) *v2.ToolBox {
	return InitS18TeammateToolbox(
		messageBus,
		protocolBook,
		taskBoard,
		agentName,
		cwdProvider,
		afterClaim,
		afterComplete,
	)
}

// InitS19Toolbox 对标 Python S19 BUILTIN_TOOLS。
//
// 迭代原因：S19 在 S18 Lead 固定工具基础上新增 connect_mcp；
// 连接后发现的 mcp__{server}__{tool} 不注册进固定 ToolBox，而由 main.go 动态合并。
//
// 与 InitS18Toolbox 差别：保留 S18 全部工具并追加 NewConnectMCPToolV2，旧 S18 工具箱不受影响。
func InitS19Toolbox(
	subAgent *subagent.SubAgent,
	skillRegistry *skills.Registry,
	taskBoard tasks.Board,
	cronScheduler *cron.Scheduler,
	spawner *team.Spawner,
	messageBus *team.MessageBus,
	protocolBook *team.ProtocolBook,
	worktreeStore *worktree.Store,
	mcpRegistry *mcp.Registry,
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

		tools.NewSpawnWorktreeAutonomousTeammateToolV2(spawner),
		tools.NewSendMessageToolV2(messageBus, "lead"),
		tools.NewCheckInboxToolV2(messageBus, protocolBook),

		tools.NewRequestShutdownToolV2(messageBus, protocolBook),
		tools.NewRequestPlanToolV2(messageBus, protocolBook),
		tools.NewReviewPlanToolV2(messageBus, protocolBook),

		tools.NewCreateWorktreeToolV2(worktreeStore, taskBoard),
		tools.NewRemoveWorktreeToolV2(worktreeStore),
		tools.NewKeepWorktreeToolV2(worktreeStore),

		tools.NewConnectMCPToolV2(mcpRegistry),
	)
}
