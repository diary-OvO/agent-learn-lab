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
	"AgentLoop/internal/worktree"

	v2 "AgentLoop/internal/toolkit/v2"
)

// InitS18Hooks 对标 S18 继承 S17 hooks。
//
// 迭代原因：S18 新增 worktree isolation，但不新增 hook 时机；权限、Stop、
// UserPromptSubmit 等 hook 仍沿用前序课程。
//
// 与 InitS17Hooks 差别：这里只提供 S18 课程入口名，明确 main.go 当前章节；
// 实际注册逻辑继续复用 S17。
func InitS18Hooks(
	hookBus *hooks.HookBus,
	checker *permission.PermissionChecker,
	workDir string,
) {
	InitS17Hooks(hookBus, checker, workDir)
}

// InitS18SubToolbox 对标 S18 常规 task subagent 仍沿用 S17。
//
// 迭代原因：worktree isolation 只作用于 team teammate；普通 task subagent
// 不是 autonomous idle loop，不需要 worktree cwd 绑定。
//
// 与 InitS17SubToolbox 差别：行为完全复用，保留 S18 命名是为了课程 main.go 可对照。
func InitS18SubToolbox() *v2.ToolBox {
	return InitS17SubToolbox()
}

// InitS18TeammateToolbox 对标 Python S18 teammate sub_tools/sub_handlers。
//
// 迭代原因：S18 teammate 在 claim 绑定 worktree 的 task 后，bash/read_file/write_file
// 必须运行在该 worktree cwd。
//
// 与 InitS17TeammateToolbox 差别：S17 使用普通文件工具；S18 替换为 WithCWD 工具，
// 并给 claim_task/complete_task 注入回调来设置或清空 cwd。
func InitS18TeammateToolbox(
	messageBus *team.MessageBus,
	protocolBook *team.ProtocolBook,
	taskBoard tasks.Board,
	agentName string,
	cwdProvider func() string,
	afterClaim func(tasks.Task),
	afterComplete func(tasks.Task),
) *v2.ToolBox {
	return v2.NewToolBox(
		tools.NewBashV2ToolV2WithCWD(cwdProvider),
		tools.NewReadFileToolV2WithCWD(cwdProvider),
		tools.NewWriteFileToolV2WithCWD(cwdProvider),

		tools.NewSendMessageToolV2(messageBus, agentName),
		tools.NewSubmitPlanToolV2(messageBus, protocolBook, agentName),

		tools.NewListTasksToolV2(taskBoard),
		tools.NewClaimTaskToolV2WithOwnerAndAfterClaim(taskBoard, agentName, afterClaim),
		tools.NewCompleteTaskToolV2WithAfterComplete(taskBoard, afterComplete),
	)
}

// InitS18Toolbox 对标 Python S18 Lead TOOLS。
//
// 迭代原因：S18 Lead 在 S17 autonomous team 基础上新增 create_worktree、
// remove_worktree、keep_worktree 三个隔离目录工具。
//
// 与 InitS17Toolbox 差别：保留 S17 所有工具，但 spawn_teammate 切到 worktree-aware
// autonomous 版本，并追加 Lead worktree tools；旧 S17 toolbox 不受影响。
func InitS18Toolbox(
	subAgent *subagent.SubAgent,
	skillRegistry *skills.Registry,
	taskBoard tasks.Board,
	cronScheduler *cron.Scheduler,
	spawner *team.Spawner,
	messageBus *team.MessageBus,
	protocolBook *team.ProtocolBook,
	worktreeStore *worktree.Store,
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
	)
}
