package team

import (
	"AgentLoop/internal/openaiadapter"
	"AgentLoop/internal/tasks"
	v2 "AgentLoop/internal/toolkit/v2"
	"AgentLoop/internal/worktree"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/openai/openai-go/v3"
)

const (
	teammateMaxRounds                  = 10
	teammateIdlePollInterval           = time.Second
	teammateAutonomousIdlePollInterval = 5 * time.Second
	teammateAutonomousIdleTimeout      = 60 * time.Second
)

type ToolboxFactory func(agentName string) *v2.ToolBox

// WorktreeToolboxFactory 对标 Python S18 teammate sub_handlers 闭包。
//
// 迭代原因：S18 teammate 的 bash/read/write 需要读取当前 cwd，claim/complete 又要回调
// Spawner 更新 cwd；Go 端用 factory 注入这些闭包，避免 team 包 import tools 包造成循环依赖。
//
// 与 ToolboxFactory 差别：S17 只按 agentName 创建固定工具箱；S18 版本还传入
// cwdProvider、afterClaim、afterComplete 三个生命周期闭包。
type WorktreeToolboxFactory func(
	agentName string,
	cwdProvider func() string,
	afterClaim func(tasks.Task),
	afterComplete func(tasks.Task),
) *v2.ToolBox

// Spawner 对标 Python active_teammates + spawn_teammate_thread。
//
// S15 使用 SpawnLimited：最多 10 轮后自然退出。
// S16 使用 SpawnPersistent：无工具调用时进入 inbox idle loop，等待后续协议或消息。
// S17 使用 SpawnAutonomous：persistent 基础上增加空闲任务扫描和自动认领。
// S18 使用 SpawnWorktreeAutonomous：S17 基础上让 teammate 的工具 cwd 跟随 task.worktree。
type Spawner struct {
	client             openai.Client
	model              string
	bus                *MessageBus
	board              tasks.Board
	worktrees          *worktree.Store
	newToolbox         ToolboxFactory
	newWorktreeToolbox WorktreeToolboxFactory

	mu     sync.Mutex
	active map[string]bool
}

// NewSpawner 对标 Python active_teammates 初始化。
//
// 创建当前 Lead 使用的 teammate spawner。
func NewSpawner(
	client openai.Client,
	model string,
	bus *MessageBus,
	newToolbox ToolboxFactory,
) *Spawner {
	return &Spawner{
		client:     client,
		model:      model,
		bus:        bus,
		newToolbox: newToolbox,
		active:     make(map[string]bool),
	}
}

// NewAutonomousSpawner 对标 Python S17 active_teammates 初始化。
//
// 创建 autonomous teammate spawner；旧 NewSpawner 保持 S15/S16 语义，不需要任务板。
// 迭代原因：S17 idle loop 必须访问任务板，单靠 S15/S16 的 MessageBus + toolboxFactory 已经不够。
// 与旧函数差别：NewSpawner 只持有通信和工具箱工厂，适合 limited/persistent；NewAutonomousSpawner 额外注入 tasks.Board，只给 autonomous auto-claim 使用。
func NewAutonomousSpawner(
	client openai.Client,
	model string,
	bus *MessageBus,
	board tasks.Board,
	newToolbox ToolboxFactory,
) *Spawner {
	return &Spawner{
		client:     client,
		model:      model,
		bus:        bus,
		board:      board,
		newToolbox: newToolbox,
		active:     make(map[string]bool),
	}
}

// NewWorktreeAutonomousSpawner 对标 Python S18 active_teammates 初始化。
//
// 迭代原因：S18 在 S17 autonomous teammate 基础上新增 worktree cwd 绑定，
// Spawner 需要同时知道任务板和 worktree store，并给 teammate 工具箱注入 cwdProvider。
//
// 与 NewAutonomousSpawner 差别：S17 构造函数只需要 task board 和普通 toolboxFactory；
// S18 版本额外注入 worktree.Store 和 WorktreeToolboxFactory，旧构造函数保持不变。
func NewWorktreeAutonomousSpawner(
	client openai.Client,
	model string,
	bus *MessageBus,
	board tasks.Board,
	worktrees *worktree.Store,
	newToolbox WorktreeToolboxFactory,
) *Spawner {
	return &Spawner{
		client:             client,
		model:              model,
		bus:                bus,
		board:              board,
		worktrees:          worktrees,
		newWorktreeToolbox: newToolbox,
		active:             make(map[string]bool),
	}
}

// SpawnLimited 对标 Python S15 spawn_teammate_thread。
//
// 教学版 teammate loop：后台启动，最多 10 轮，完成后向 Lead 发送最终 summary。
func (s *Spawner) SpawnLimited(
	ctx context.Context,
	name string,
	role string,
	initialPrompt string,
) (string, error) {
	name, role, initialPrompt, reserved, err := s.reserveTeammate(
		name,
		role,
		initialPrompt,
	)
	if err != nil {
		return "", err
	}
	if !reserved {
		return fmt.Sprintf("Teammate %q already exists", name), nil
	}

	system := teammateSystemPrompt(name, role, false)

	go s.runLimitedTeammate(ctx, name, system, initialPrompt)

	fmt.Printf(
		"  \033[36m[teammate] %s spawned as %s\033[0m\n",
		name,
		role,
	)

	return fmt.Sprintf("Teammate %q spawned as %s", name, role), nil
}

// SpawnPersistent 对标 Python S16 spawn_teammate_thread。
//
// 持续版 teammate loop：无工具调用时不自然退出，而是 idle 等待 inbox。
func (s *Spawner) SpawnPersistent(
	ctx context.Context,
	name string,
	role string,
	initialPrompt string,
) (string, error) {
	name, role, initialPrompt, reserved, err := s.reserveTeammate(
		name,
		role,
		initialPrompt,
	)
	if err != nil {
		return "", err
	}
	if !reserved {
		return fmt.Sprintf("Teammate %q already exists", name), nil
	}

	system := teammateSystemPrompt(name, role, true)

	go s.runPersistentTeammate(ctx, name, system, initialPrompt)

	fmt.Printf(
		"  \033[36m[teammate] %s spawned as %s (persistent)\033[0m\n",
		name,
		role,
	)

	return fmt.Sprintf("Teammate %q spawned as %s (persistent)", name, role), nil
}

// SpawnAutonomous 对标 Python S17 spawn_teammate_thread。
//
// 自主版 teammate loop：WORK → IDLE → SHUTDOWN，IDLE 阶段会扫描任务板并自动认领可开始任务。
// 迭代原因：S16 的 SpawnPersistent 只能等 Lead 或协议消息唤醒，teammate 不会主动寻找任务板上的工作。
// 与旧函数差别：SpawnLimited 最多 10 轮后退出；SpawnPersistent 无工具调用时只等 inbox；SpawnAutonomous 在等待 inbox 的同时会扫描并认领 unclaimed task。
func (s *Spawner) SpawnAutonomous(
	ctx context.Context,
	name string,
	role string,
	initialPrompt string,
) (string, error) {
	name, role, initialPrompt, reserved, err := s.reserveTeammate(
		name,
		role,
		initialPrompt,
	)
	if err != nil {
		return "", err
	}
	if !reserved {
		return fmt.Sprintf("Teammate %q already exists", name), nil
	}

	system := teammateAutonomousSystemPrompt(name, role)

	go s.runAutonomousTeammate(ctx, name, role, system, initialPrompt)

	fmt.Printf(
		"  \033[36m[teammate] %s spawned as %s (autonomous)\033[0m\n",
		name,
		role,
	)

	return fmt.Sprintf("Teammate %q spawned as %s (autonomous)", name, role), nil
}

// SpawnWorktreeAutonomous 对标 Python S18 spawn_teammate_thread。
//
// 迭代原因：S18 teammate 仍是 autonomous lifecycle，但认领绑定 worktree 的 task 后，
// bash/read_file/write_file 必须切到对应 .worktrees/{name}。
//
// 与 SpawnAutonomous 差别：S17 只自动认领任务并在主工作区执行工具；S18 版本增加
// task.worktree -> current cwd 绑定，旧 SpawnAutonomous 保持原逻辑。
func (s *Spawner) SpawnWorktreeAutonomous(
	ctx context.Context,
	name string,
	role string,
	initialPrompt string,
) (string, error) {
	if s == nil {
		return "", fmt.Errorf("teammate spawner is nil")
	}
	if s.worktrees == nil {
		return "", fmt.Errorf("worktree store is nil")
	}
	if s.newWorktreeToolbox == nil {
		return "", fmt.Errorf("worktree toolbox factory is nil")
	}

	name, role, initialPrompt, reserved, err := s.reserveTeammate(
		name,
		role,
		initialPrompt,
	)
	if err != nil {
		return "", err
	}
	if !reserved {
		return fmt.Sprintf("Teammate %q already exists", name), nil
	}

	system := teammateWorktreeAutonomousSystemPrompt(name, role)

	go s.runWorktreeAutonomousTeammate(ctx, name, role, system, initialPrompt)

	fmt.Printf(
		"  \033[36m[teammate] %s spawned as %s (worktree autonomous)\033[0m\n",
		name,
		role,
	)

	return fmt.Sprintf("Teammate %q spawned as %s (worktree autonomous)", name, role), nil
}

// HasActive 对标 Python active_teammates 非空判断。
//
// 用于 Lead 侧只在所有 teammate 完成且 inbox/background 已清空后打印完成提示。
func (s *Spawner) HasActive() bool {
	if s == nil {
		return false
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	return len(s.active) > 0
}

func (s *Spawner) reserveTeammate(
	name string,
	role string,
	initialPrompt string,
) (string, string, string, bool, error) {
	if s == nil {
		return "", "", "", false, fmt.Errorf("teammate spawner is nil")
	}

	name = strings.TrimSpace(name)
	role = strings.TrimSpace(role)
	initialPrompt = strings.TrimSpace(initialPrompt)

	if name == "" {
		return "", "", "", false, fmt.Errorf("name is required")
	}
	if role == "" {
		return "", "", "", false, fmt.Errorf("role is required")
	}
	if initialPrompt == "" {
		return "", "", "", false, fmt.Errorf("prompt is required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.active[name] {
		return name, role, initialPrompt, false, nil
	}

	s.active[name] = true

	return name, role, initialPrompt, true, nil
}

func teammateSystemPrompt(name string, role string, persistent bool) string {
	prompt := fmt.Sprintf(
		"You are %q, a %s. Use tools to complete tasks. Send results via send_message to 'lead'.",
		name,
		role,
	)

	if persistent {
		prompt += " When asked for a plan, use submit_plan and wait for plan_approval_response before continuing. When shutdown_request arrives, stop gracefully."
	}

	return prompt
}

// teammateAutonomousSystemPrompt 构造 S17 teammate system prompt。
//
// 迭代原因：S17 teammate 需要知道自己可以在 idle 时查看任务板，而 S16 prompt 只描述 submit_plan / shutdown 协议。
// 与旧函数差别：teammateSystemPrompt(persistent=true) 只要求等待 inbox 和处理协议；teammateAutonomousSystemPrompt 额外强调 task board auto-claim。
func teammateAutonomousSystemPrompt(name string, role string) string {
	return fmt.Sprintf(
		"You are %q, a %s. Use tools to complete tasks. Send results via send_message to 'lead'. "+
			"When asked for a plan, use submit_plan and wait for plan_approval_response before continuing. "+
			"When shutdown_request arrives, stop gracefully. "+
			"When idle, inspect the task board, claim unowned unblocked tasks for yourself, complete them, and report progress.",
		name,
		role,
	)
}

// teammateWorktreeAutonomousSystemPrompt 构造 S18 teammate system prompt。
//
// 迭代原因：S18 teammate 除了自驱任务板，还必须理解“绑定 worktree 的任务要在该目录工作”。
//
// 与 teammateAutonomousSystemPrompt 差别：S17 prompt 只说明 idle claim；S18 prompt
// 额外说明 task.worktree 对工具 cwd 的影响。
func teammateWorktreeAutonomousSystemPrompt(name string, role string) string {
	return fmt.Sprintf(
		"You are %q, a %s. Use tools to complete tasks. Send results via send_message to 'lead'. "+
			"When asked for a plan, use submit_plan and wait for plan_approval_response before continuing. "+
			"When shutdown_request arrives, stop gracefully. "+
			"When idle, inspect the task board, claim unowned unblocked tasks for yourself, complete them, and report progress. "+
			"If a claimed task has a worktree, your bash/read_file/write_file tools run in that worktree directory.",
		name,
		role,
	)
}

func (s *Spawner) markDone(name string) {
	s.mu.Lock()
	delete(s.active, name)
	s.mu.Unlock()
}

func (s *Spawner) runLimitedTeammate(
	ctx context.Context,
	name string,
	system string,
	initialPrompt string,
) {
	defer s.finishTeammate(name)

	toolbox, chatTools, err := s.teammateToolbox(name)
	if err != nil {
		_ = s.bus.Send(name, "lead", "Failed to initialize teammate tools: "+err.Error(), "error")
		return
	}

	messages := []openai.ChatCompletionMessageParamUnion{
		openai.UserMessage(initialPrompt),
	}

	for round := 0; round < teammateMaxRounds; round++ {
		inbox, err := s.bus.ReadInbox(name)
		if err == nil && len(inbox) > 0 {
			messages = appendInboxMessage(messages, inbox)
		}

		var usedTools bool

		messages, usedTools, err = s.runOneTeammateTurn(
			ctx,
			system,
			messages,
			toolbox,
			chatTools,
		)
		if err != nil {
			_ = s.bus.Send(name, "lead", "Teammate error: "+err.Error(), "error")
			return
		}

		if !usedTools {
			break
		}
	}

	s.sendSummary(name, messages)
}

func (s *Spawner) runPersistentTeammate(
	ctx context.Context,
	name string,
	system string,
	initialPrompt string,
) {
	defer s.finishTeammate(name)

	toolbox, chatTools, err := s.teammateToolbox(name)
	if err != nil {
		_ = s.bus.Send(name, "lead", "Failed to initialize teammate tools: "+err.Error(), "error")
		return
	}

	messages := []openai.ChatCompletionMessageParamUnion{
		openai.UserMessage(initialPrompt),
	}

	for {
		inbox, err := s.bus.ReadInbox(name)
		if err == nil && len(inbox) > 0 {
			var shouldRun bool
			var shouldStop bool

			messages, shouldRun, shouldStop = s.applyPersistentInbox(
				name,
				messages,
				inbox,
			)
			if shouldStop {
				break
			}
			if !shouldRun {
				continue
			}
		}

		var usedTools bool

		messages, usedTools, err = s.runOneTeammateTurn(
			ctx,
			system,
			messages,
			toolbox,
			chatTools,
		)
		if err != nil {
			_ = s.bus.Send(name, "lead", "Teammate error: "+err.Error(), "error")
			return
		}

		if usedTools {
			continue
		}

		var shouldContinue bool
		messages, shouldContinue = s.waitForPersistentInbox(ctx, name, messages)
		if !shouldContinue {
			break
		}
	}

	s.sendSummary(name, messages)
}

// runAutonomousTeammate 对标 Python S17 teammate lifecycle。
//
// 迭代原因：S16 的 runPersistentTeammate 没有任务板自驱阶段，模型一旦没有工具调用就只能等待下一封 inbox。
// 与旧函数差别：runPersistentTeammate 的 idle 只处理 inbox；runAutonomousTeammate 在每段 WORK 后进入 idlePollAutonomous，能从任务板自动拿新任务继续工作。
func (s *Spawner) runAutonomousTeammate(
	ctx context.Context,
	name string,
	role string,
	system string,
	initialPrompt string,
) {
	defer s.finishTeammate(name)

	toolbox, chatTools, err := s.teammateToolbox(name)
	if err != nil {
		_ = s.bus.Send(name, "lead", "Failed to initialize teammate tools: "+err.Error(), "error")
		return
	}

	messages := []openai.ChatCompletionMessageParamUnion{
		openai.UserMessage(initialPrompt),
	}

	for {
		messages = ensureTeammateIdentity(messages, name, role)

		shouldStop := false

		for round := 0; round < teammateMaxRounds; round++ {
			inbox, err := s.bus.ReadInbox(name)
			if err == nil && len(inbox) > 0 {
				var shouldRun bool

				messages, shouldRun, shouldStop = s.applyPersistentInbox(
					name,
					messages,
					inbox,
				)
				if shouldStop {
					break
				}
				if !shouldRun {
					continue
				}
			}

			var usedTools bool

			messages, usedTools, err = s.runOneTeammateTurn(
				ctx,
				system,
				messages,
				toolbox,
				chatTools,
			)
			if err != nil {
				_ = s.bus.Send(name, "lead", "Teammate error: "+err.Error(), "error")
				return
			}

			if !usedTools {
				break
			}
		}

		if shouldStop {
			break
		}

		var shouldContinue bool
		messages, shouldContinue = s.idlePollAutonomous(ctx, name, role, messages)
		if !shouldContinue {
			break
		}
	}

	s.sendSummary(name, messages)
}

// runWorktreeAutonomousTeammate 对标 Python S18 teammate lifecycle。
//
// 迭代原因：S18 在 S17 WORK -> IDLE 循环上增加 current worktree cwd；
// teammate 认领绑定 worktree 的任务后，工具箱中的 bash/read/write 都应切到该目录。
//
// 与 runAutonomousTeammate 差别：S17 工具箱固定在主工作区；S18 版本为工具箱注入
// cwdProvider、afterClaim、afterComplete，让 cwd 随任务认领和完成而变化。
func (s *Spawner) runWorktreeAutonomousTeammate(
	ctx context.Context,
	name string,
	role string,
	system string,
	initialPrompt string,
) {
	defer s.finishTeammate(name)

	currentCWD := ""

	setCWDFromTask := func(task tasks.Task) {
		currentCWD = ""

		if task.Worktree == nil || strings.TrimSpace(*task.Worktree) == "" {
			return
		}
		if s.worktrees == nil {
			fmt.Printf(
				"  \033[33m[worktree cwd] %s skipped: worktree store is nil\033[0m\n",
				name,
			)
			return
		}

		path, err := s.worktrees.Path(*task.Worktree)
		if err != nil {
			fmt.Printf(
				"  \033[33m[worktree cwd] %s skipped: %v\033[0m\n",
				name,
				err,
			)
			return
		}

		currentCWD = path

		fmt.Printf(
			"  \033[33m[worktree cwd] %s -> %s\033[0m\n",
			name,
			currentCWD,
		)
	}

	clearCWD := func(_ tasks.Task) {
		if currentCWD != "" {
			fmt.Printf(
				"  \033[33m[worktree cwd] %s -> main workspace\033[0m\n",
				name,
			)
		}

		currentCWD = ""
	}

	cwdProvider := func() string {
		return currentCWD
	}

	toolbox, chatTools, err := s.teammateWorktreeToolbox(
		name,
		cwdProvider,
		setCWDFromTask,
		clearCWD,
	)
	if err != nil {
		_ = s.bus.Send(name, "lead", "Failed to initialize teammate tools: "+err.Error(), "error")
		return
	}

	messages := []openai.ChatCompletionMessageParamUnion{
		openai.UserMessage(initialPrompt),
	}

	for {
		messages = ensureTeammateIdentity(messages, name, role)

		shouldStop := false

		for round := 0; round < teammateMaxRounds; round++ {
			inbox, err := s.bus.ReadInbox(name)
			if err == nil && len(inbox) > 0 {
				var shouldRun bool

				messages, shouldRun, shouldStop = s.applyPersistentInbox(
					name,
					messages,
					inbox,
				)
				if shouldStop {
					break
				}
				if !shouldRun {
					continue
				}
			}

			var usedTools bool

			messages, usedTools, err = s.runOneTeammateTurn(
				ctx,
				system,
				messages,
				toolbox,
				chatTools,
			)
			if err != nil {
				_ = s.bus.Send(name, "lead", "Teammate error: "+err.Error(), "error")
				return
			}

			if !usedTools {
				break
			}
		}

		if shouldStop {
			break
		}

		var shouldContinue bool
		messages, shouldContinue = s.idlePollWorktreeAutonomous(
			ctx,
			name,
			role,
			messages,
			setCWDFromTask,
		)
		if !shouldContinue {
			break
		}
	}

	s.sendSummary(name, messages)
}

// teammateWorktreeToolbox 对标 Python S18 sub_tools/sub_handlers 构造。
//
// 迭代原因：S18 teammate 工具箱需要携带当前 cwd 和 claim/complete 回调，普通
// teammateToolbox(name) 已无法表达这些运行时闭包。
//
// 与 teammateToolbox 差别：旧 helper 只按 agentName 生成固定工具箱；这里额外注入
// cwdProvider、afterClaim、afterComplete，专供 SpawnWorktreeAutonomous 使用。
func (s *Spawner) teammateWorktreeToolbox(
	name string,
	cwdProvider func() string,
	afterClaim func(tasks.Task),
	afterComplete func(tasks.Task),
) (*v2.ToolBox, []openai.ChatCompletionToolUnionParam, error) {
	if s.newWorktreeToolbox == nil {
		return nil, nil, fmt.Errorf("worktree toolbox factory is nil")
	}

	toolbox := s.newWorktreeToolbox(name, cwdProvider, afterClaim, afterComplete)
	if toolbox == nil {
		return nil, nil, fmt.Errorf("toolbox is nil")
	}

	chatTools, err := openaiadapter.ToChatCompletionToolsV2(toolbox.Schemas())
	if err != nil {
		return nil, nil, err
	}

	return toolbox, chatTools, nil
}

func (s *Spawner) teammateToolbox(
	name string,
) (*v2.ToolBox, []openai.ChatCompletionToolUnionParam, error) {
	if s.newToolbox == nil {
		return nil, nil, fmt.Errorf("toolbox factory is nil")
	}

	toolbox := s.newToolbox(name)
	if toolbox == nil {
		return nil, nil, fmt.Errorf("toolbox is nil")
	}

	chatTools, err := openaiadapter.ToChatCompletionToolsV2(toolbox.Schemas())
	if err != nil {
		return nil, nil, err
	}

	return toolbox, chatTools, nil
}

func (s *Spawner) runOneTeammateTurn(
	ctx context.Context,
	system string,
	messages []openai.ChatCompletionMessageParamUnion,
	toolbox *v2.ToolBox,
	chatTools []openai.ChatCompletionToolUnionParam,
) ([]openai.ChatCompletionMessageParamUnion, bool, error) {
	response, err := s.client.Chat.Completions.New(
		ctx,
		openai.ChatCompletionNewParams{
			Model: s.model,
			Messages: append(
				[]openai.ChatCompletionMessageParamUnion{
					openai.SystemMessage(system),
				},
				safeRecent(messages, 20)...,
			),
			Tools:               chatTools,
			MaxCompletionTokens: openai.Int(8000),
		},
	)
	if err != nil {
		return messages, false, err
	}

	if len(response.Choices) == 0 {
		return messages, false, fmt.Errorf("empty response")
	}

	msg := response.Choices[0].Message
	messages = append(messages, msg.ToParam())

	if len(msg.ToolCalls) == 0 {
		return messages, false, nil
	}

	for _, toolCall := range msg.ToolCalls {
		call := v2.ToolCall{
			Name:      toolCall.Function.Name,
			Arguments: json.RawMessage(toolCall.Function.Arguments),
		}

		result, err := toolbox.Execute(ctx, call)
		if err != nil {
			result = fmt.Sprintf(`{"error": %q}`, err.Error())
		}

		messages = append(
			messages,
			openai.ToolMessage(result, toolCall.ID),
		)
	}

	return messages, true, nil
}

func (s *Spawner) waitForPersistentInbox(
	ctx context.Context,
	name string,
	messages []openai.ChatCompletionMessageParamUnion,
) ([]openai.ChatCompletionMessageParamUnion, bool) {
	ticker := time.NewTicker(teammateIdlePollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return messages, false
		case <-ticker.C:
			inbox, err := s.bus.ReadInbox(name)
			if err != nil || len(inbox) == 0 {
				continue
			}

			var shouldRun bool
			var shouldStop bool

			messages, shouldRun, shouldStop = s.applyPersistentInbox(
				name,
				messages,
				inbox,
			)
			if shouldStop {
				return messages, false
			}
			if shouldRun {
				return messages, true
			}
		}
	}
}

// idlePollAutonomous 对标 Python S17 idle_poll。
//
// IDLE 阶段每 5 秒优先检查 inbox，其次扫描任务板并尝试认领可开始任务。
// 迭代原因：S17 要把 teammate 从“被 Lead 分配任务”推进到“空闲时自己找任务”。
// 与旧函数差别：waitForPersistentInbox 永远只读 inbox；idlePollAutonomous 先读 inbox，再调用 claimNextAutonomousTask 扫描任务板，并带 60 秒 idle timeout。
func (s *Spawner) idlePollAutonomous(
	ctx context.Context,
	name string,
	role string,
	messages []openai.ChatCompletionMessageParamUnion,
) ([]openai.ChatCompletionMessageParamUnion, bool) {
	ticker := time.NewTicker(teammateAutonomousIdlePollInterval)
	defer ticker.Stop()

	timeout := time.NewTimer(teammateAutonomousIdleTimeout)
	defer timeout.Stop()

	for {
		select {
		case <-ctx.Done():
			return messages, false

		case <-timeout.C:
			fmt.Printf(
				"  \033[31m[idle] %s timeout (%s)\033[0m\n",
				name,
				teammateAutonomousIdleTimeout,
			)

			return messages, false

		case <-ticker.C:
			inbox, err := s.bus.ReadInbox(name)
			if err == nil && len(inbox) > 0 {
				var shouldRun bool
				var shouldStop bool

				messages, shouldRun, shouldStop = s.applyPersistentInbox(
					name,
					messages,
					inbox,
				)
				if shouldStop {
					return messages, false
				}
				if shouldRun {
					fmt.Printf(
						"  \033[36m[idle] %s found inbox work\033[0m\n",
						name,
					)

					return ensureTeammateIdentity(messages, name, role), true
				}
			}

			task, claimed, err := s.claimNextAutonomousTask(name)
			if err != nil {
				fmt.Printf(
					"  \033[33m[idle] %s scan failed: %v\033[0m\n",
					name,
					err,
				)

				continue
			}
			if !claimed {
				continue
			}

			messages = append(
				messages,
				openai.UserMessage(fmt.Sprintf(
					"[Autonomous task claimed]\nTask %s: %s\n\n%s",
					task.ID,
					task.Subject,
					task.Description,
				)),
			)

			fmt.Printf(
				"  \033[32m[idle] %s auto-claimed: %s\033[0m\n",
				name,
				task.Subject,
			)

			return ensureTeammateIdentity(messages, name, role), true
		}
	}
}

// idlePollWorktreeAutonomous 对标 Python S18 idle_poll。
//
// 迭代原因：S18 IDLE 阶段自动 claim 后，如果 task.worktree 存在，需要把 teammate
// 的工具 cwd 切到对应 worktree，并把工作目录提示注入给模型。
//
// 与 idlePollAutonomous 差别：S17 只追加任务描述；S18 版本在 claim 成功后调用
// afterClaim，并在消息里附带 Work directory。
func (s *Spawner) idlePollWorktreeAutonomous(
	ctx context.Context,
	name string,
	role string,
	messages []openai.ChatCompletionMessageParamUnion,
	afterClaim func(tasks.Task),
) ([]openai.ChatCompletionMessageParamUnion, bool) {
	ticker := time.NewTicker(teammateAutonomousIdlePollInterval)
	defer ticker.Stop()

	timeout := time.NewTimer(teammateAutonomousIdleTimeout)
	defer timeout.Stop()

	for {
		select {
		case <-ctx.Done():
			return messages, false

		case <-timeout.C:
			fmt.Printf(
				"  \033[31m[idle] %s timeout (%s)\033[0m\n",
				name,
				teammateAutonomousIdleTimeout,
			)

			return messages, false

		case <-ticker.C:
			inbox, err := s.bus.ReadInbox(name)
			if err == nil && len(inbox) > 0 {
				var shouldRun bool
				var shouldStop bool

				messages, shouldRun, shouldStop = s.applyPersistentInbox(
					name,
					messages,
					inbox,
				)
				if shouldStop {
					return messages, false
				}
				if shouldRun {
					fmt.Printf(
						"  \033[36m[idle] %s found inbox work\033[0m\n",
						name,
					)

					return ensureTeammateIdentity(messages, name, role), true
				}
			}

			task, claimed, err := s.claimNextWorktreeTask(name)
			if err != nil {
				fmt.Printf(
					"  \033[33m[idle] %s scan failed: %v\033[0m\n",
					name,
					err,
				)

				continue
			}
			if !claimed {
				continue
			}

			if afterClaim != nil {
				afterClaim(task)
			}

			worktreeInfo := ""
			if task.Worktree != nil && strings.TrimSpace(*task.Worktree) != "" && s.worktrees != nil {
				if path, err := s.worktrees.Path(*task.Worktree); err == nil {
					worktreeInfo = "\nWork directory: " + path
				}
			}

			messages = append(
				messages,
				openai.UserMessage(fmt.Sprintf(
					"[Autonomous task claimed]\nTask %s: %s%s\n\n%s",
					task.ID,
					task.Subject,
					worktreeInfo,
					task.Description,
				)),
			)

			fmt.Printf(
				"  \033[32m[idle] %s auto-claimed: %s\033[0m\n",
				name,
				task.Subject,
			)

			return ensureTeammateIdentity(messages, name, role), true
		}
	}
}

// claimNextAutonomousTask 从任务板中认领一个 S17 autonomous task。
//
// 迭代原因：idlePollAutonomous 需要一个可复用的小步骤，把“扫描候选任务”和“带 owner 检查认领”合在一起。
// 与旧函数差别：旧路径由模型显式调用 claim_task；这里由 Spawner 在 idle 阶段自动调用 Board.ScanUnclaimed 和 ClaimWithOwnerCheck。
func (s *Spawner) claimNextAutonomousTask(name string) (tasks.Task, bool, error) {
	if strings.TrimSpace(s.board.Dir) == "" {
		return tasks.Task{}, false, nil
	}

	unclaimed, err := s.board.ScanUnclaimed()
	if err != nil {
		return tasks.Task{}, false, err
	}

	for _, task := range unclaimed {
		result, err := s.board.ClaimWithOwnerCheck(task.ID, name)
		if err != nil {
			return tasks.Task{}, false, err
		}

		if strings.Contains(result, "Claimed") {
			return task, true, nil
		}

		fmt.Printf(
			"  \033[33m[idle] %s claim skipped: %s\033[0m\n",
			name,
			result,
		)
	}

	return tasks.Task{}, false, nil
}

// claimNextWorktreeTask 从任务板中认领一个 S18 autonomous task。
//
// 迭代原因：S18 auto-claim 成功后需要拿到已经持久化后的 task.worktree，
// 以便切换 cwd 和注入 Work directory。
//
// 与 claimNextAutonomousTask 差别：S17 返回扫描到的原始 task；S18 版本在 ClaimWithOwnerCheck
// 成功后重新 Load task，确保拿到最新 owner/status/worktree 字段。
func (s *Spawner) claimNextWorktreeTask(name string) (tasks.Task, bool, error) {
	if strings.TrimSpace(s.board.Dir) == "" {
		return tasks.Task{}, false, nil
	}

	unclaimed, err := s.board.ScanUnclaimed()
	if err != nil {
		return tasks.Task{}, false, err
	}

	for _, task := range unclaimed {
		result, err := s.board.ClaimWithOwnerCheck(task.ID, name)
		if err != nil {
			return tasks.Task{}, false, err
		}

		if !strings.Contains(result, "Claimed") {
			fmt.Printf(
				"  \033[33m[idle] %s claim skipped: %s\033[0m\n",
				name,
				result,
			)

			continue
		}

		claimed, err := s.board.Load(task.ID)
		if err != nil {
			return task, true, nil
		}

		return claimed, true, nil
	}

	return tasks.Task{}, false, nil
}

func (s *Spawner) applyPersistentInbox(
	name string,
	messages []openai.ChatCompletionMessageParamUnion,
	inbox []Message,
) ([]openai.ChatCompletionMessageParamUnion, bool, bool) {
	normalMessages := make([]Message, 0, len(inbox))
	shouldRun := false

	for _, msg := range inbox {
		switch msg.Type {
		case "shutdown_request":
			reqID := MetaString(msg.Metadata, "request_id")

			_ = s.bus.SendWithMetadata(
				name,
				"lead",
				"Shutting down gracefully.",
				"shutdown_response",
				map[string]any{
					"request_id": reqID,
					"approve":    true,
				},
			)

			return messages, false, true

		case "plan_approval_response":
			reqID := MetaString(msg.Metadata, "request_id")
			approved := MetaBool(msg.Metadata, "approve")
			status := "rejected"
			if approved {
				status = "approved"
			}

			feedback := strings.TrimSpace(msg.Content)
			if feedback == "" {
				feedback = status
			}

			messages = append(
				messages,
				openai.UserMessage(
					fmt.Sprintf(
						"[Plan %s]\nRequest: %s\nFeedback: %s",
						status,
						reqID,
						feedback,
					),
				),
			)

			shouldRun = true

		default:
			normalMessages = append(normalMessages, msg)
		}
	}

	if len(normalMessages) > 0 {
		messages = appendInboxMessage(messages, normalMessages)
		shouldRun = true
	}

	return messages, shouldRun, false
}

func (s *Spawner) finishTeammate(name string) {
	s.markDone(name)

	fmt.Printf(
		"  \033[32m[teammate] %s finished\033[0m\n",
		name,
	)
}

func (s *Spawner) sendSummary(
	name string,
	messages []openai.ChatCompletionMessageParamUnion,
) {
	summary := "Done."

	for i := len(messages) - 1; i >= 0; i-- {
		if openaiadapter.MessageRole(messages[i]) != "assistant" {
			continue
		}

		text := strings.TrimSpace(openaiadapter.MessageTextContent(messages[i]))
		if text != "" {
			summary = text
			break
		}
	}

	_ = s.bus.Send(name, "lead", summary, "result")
}

func appendInboxMessage(
	messages []openai.ChatCompletionMessageParamUnion,
	inbox []Message,
) []openai.ChatCompletionMessageParamUnion {
	raw, _ := json.Marshal(inbox)

	return append(
		messages,
		openai.UserMessage("[Inbox]\n"+string(raw)),
	)
}

// ensureTeammateIdentity 对标 Python S17 压缩后身份重注入。
//
// 迭代原因：autonomous teammate 可能经历多轮 WORK/IDLE，历史被裁剪后容易忘记自己的 name/role。
// 与旧函数差别：S15/S16 teammate 只依赖 system prompt 和最近消息；S17 在消息历史中补一条 identity user message，让 safeRecent 后仍保留身份锚点。
func ensureTeammateIdentity(
	messages []openai.ChatCompletionMessageParamUnion,
	name string,
	role string,
) []openai.ChatCompletionMessageParamUnion {
	for i := len(messages) - 1; i >= 0 && i >= len(messages)-5; i-- {
		if strings.Contains(
			openaiadapter.MessageTextContent(messages[i]),
			"<teammate_identity>",
		) {
			return messages
		}
	}

	return append(
		messages,
		openai.UserMessage(fmt.Sprintf(
			"<teammate_identity>You are %q, role: %s. Keep this identity after context trimming or compaction.</teammate_identity>",
			name,
			role,
		)),
	)
}

func safeRecent(
	messages []openai.ChatCompletionMessageParamUnion,
	n int,
) []openai.ChatCompletionMessageParamUnion {
	if n <= 0 || len(messages) <= n {
		return openaiadapter.CloneMessages(messages)
	}

	return openaiadapter.CloneMessages(messages[len(messages)-n:])
}
