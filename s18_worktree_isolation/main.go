package main

import (
	"AgentLoop/internal/agentconsole"
	"AgentLoop/internal/background"
	"AgentLoop/internal/compact"
	"AgentLoop/internal/cron"
	"AgentLoop/internal/hooks"
	"AgentLoop/internal/loopinit"
	"AgentLoop/internal/memory"
	"AgentLoop/internal/modelclient"
	"AgentLoop/internal/openaiadapter"
	"AgentLoop/internal/permission"
	"AgentLoop/internal/prompt"
	"AgentLoop/internal/recovery"
	"AgentLoop/internal/skills"
	"AgentLoop/internal/subagent"
	"AgentLoop/internal/tasks"
	"AgentLoop/internal/team"
	"AgentLoop/internal/worktree"

	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	v2 "AgentLoop/internal/toolkit/v2"

	"github.com/openai/openai-go/v3"
)

const (
	modelID           = "deepseek-v4-pro"
	fallbackModelID   = "qwen3.7-plus"
	compactToolName   = "compact"
	maxAgentLoopSteps = 20
)

type leadEvent struct {
	kind string
	text string
}

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	client, _, err := modelclient.NewFromEnv(modelclient.Aliyun())
	if err != nil {
		panic(err)
	}

	ctx = agentconsole.WithAgentScope(ctx, agentconsole.AgentScope{
		Name:  "lead",
		ID:    "lead",
		Depth: 0,
	})

	reader := bufio.NewReader(os.Stdin)
	checker := permission.NewPermissionCheckerWithReader(reader)

	workdir, err := os.Getwd()
	if err != nil {
		panic(err)
	}

	memoryLibrary, err := memory.NewLibrary(workdir)
	if err != nil {
		panic(err)
	}

	taskBoard, err := tasks.NewBoard(workdir)
	if err != nil {
		panic(err)
	}

	// S18 新增：Worktree Store 对标 Python WORKTREES_DIR。
	//
	// 只负责 .worktrees、events.jsonl 和 git worktree 命令，不接管 Agent Loop。
	worktreeStore, err := worktree.NewStore(workdir)
	if err != nil {
		panic(err)
	}

	// S16 继承 S15：cron.Scheduler 仍只负责计划任务状态和触发队列。
	//
	// 对照 Python S16：team protocol 只改变 team 消息语义，不改变 cron_queue 的消费方式。
	cronScheduler, err := cron.NewScheduler(workdir)
	if err != nil {
		panic(err)
	}

	// S16 继承 S15：MessageBus 对标 Python MessageBus。
	//
	// Lead 和 teammate 仍通过 .mailboxes/*.jsonl 交换消息；S16 在 message metadata 中加入 request_id。
	messageBus, err := team.NewMessageBus(workdir)
	if err != nil {
		panic(err)
	}

	// S16 新增：ProtocolBook 对标 Python pending_requests。
	//
	// 只记录当前进程内等待响应的 shutdown / plan_approval request，不接管 Agent Loop。
	protocolBook := team.NewProtocolBook()

	hookBus := hooks.NewHookBus()
	// S18 改动：hooks 继承 S17，不新增 hook 类型。
	loopinit.InitS18Hooks(hookBus, checker, workdir)

	bgTracker := background.NewTracker()

	skillsDir := filepath.Join(workdir, "skills")
	skillRegistry, err := skills.Scan(skillsDir)
	if err != nil {
		panic(err)
	}

	// S18 改动：常规 task subagent 仍沿用 S17 工具箱。
	subToolbox := loopinit.InitS18SubToolbox()

	subAgent, err := subagent.New(client, subToolbox, hookBus)
	if err != nil {
		panic(err)
	}

	// S18 改动：Spawner 使用 worktree-aware autonomous 版本。
	//
	// autonomous teammate 在 IDLE 阶段会 scan_unclaimed_tasks 并自动 claim；
	// 如果 task.worktree 非空，teammate 的 bash/read/write 工具会切到对应 worktree cwd。
	spawner := team.NewWorktreeAutonomousSpawner(
		client,
		modelID,
		messageBus,
		taskBoard,
		worktreeStore,
		func(
			agentName string,
			cwdProvider func() string,
			afterClaim func(tasks.Task),
			afterComplete func(tasks.Task),
		) *v2.ToolBox {
			return loopinit.InitS18TeammateToolbox(
				messageBus,
				protocolBook,
				taskBoard,
				agentName,
				cwdProvider,
				afterClaim,
				afterComplete,
			)
		},
	)

	// S18 改动：Lead 工具箱继承 S17，并新增 create/remove/keep_worktree。
	toolbox := loopinit.InitS18Toolbox(
		subAgent,
		skillRegistry,
		taskBoard,
		cronScheduler,
		spawner,
		messageBus,
		protocolBook,
		worktreeStore,
	)

	schemas := append(toolbox.Schemas(), compact.CompactToolSchema())
	chatTools, err := openaiadapter.ToChatCompletionToolsV2(schemas)
	if err != nil {
		panic(err)
	}

	enabledTools := v2.SchemaNames(schemas)
	// S18 继承 S17：system prompt 使用 V2 section 列表。
	//
	// S18 在 autonomous_team 后追加 worktree_isolation section。
	promptSections := prompt.S18SectionsV2()
	var promptCache prompt.CacheV2

	promptContext, err := prompt.UpdateContext(
		workdir,
		enabledTools,
		skillRegistry.List(),
		memoryLibrary,
	)
	if err != nil {
		panic(err)
	}

	messages := []openai.ChatCompletionMessageParamUnion{
		openai.SystemMessage(promptCache.Get(promptContext, promptSections)),
	}

	// S16 继承 S15 的 cron scheduler goroutine。
	//
	// 它只把到期任务放入 cron queue；S16 的 Lead turn 仍被用户输入或 inbox/background wake 触发。
	cron.StartScheduler(ctx, cronScheduler)
	fmt.Printf("  \033[35m[cron] scheduler started (%s)\033[0m\n", cronScheduler.DurablePath())

	events := make(chan leadEvent, 16)

	fmt.Println("s18: worktree isolation")
	fmt.Println("Enter a question, press Enter to send. Type q to quit.")
	fmt.Println()

	// S16 继承 S15：input_reader 对标 Python input_reader。
	//
	// 用户输入不直接运行 Agent，而是投递到同一个事件队列。
	startInputReader(ctx, reader, events)

	// S16 继承 S15：inbox_poller 对标 Python inbox_poller。
	//
	// 这里只 Peek，不读取 inbox；真正读取集中在 collectInboxNotifications 里完成协议路由。
	startInboxPoller(ctx, messageBus, bgTracker, events)

	hadTeammates := false

	for {
		select {
		case <-ctx.Done():
			return

		case event := <-events:
			switch event.kind {
			case "quit":
				return

			case "user":
				query := strings.TrimSpace(event.text)
				if query == "" ||
					strings.EqualFold(query, "q") ||
					strings.EqualFold(query, "quit") ||
					strings.EqualFold(query, "exit") {
					return
				}

				hookedQuery := hookBus.TriggerUserPromptSubmit(ctx, query)
				if strings.TrimSpace(hookedQuery) != "" {
					query = hookedQuery
				}

				answer, nextMessages, err := runAgentTurn(
					ctx,
					client,
					chatTools,
					toolbox,
					hookBus,
					cronScheduler,
					bgTracker,
					messageBus,
					protocolBook,
					memoryLibrary,
					&promptCache,
					promptSections,
					enabledTools,
					skillRegistry.List(),
					workdir,
					query,
					messages,
				)
				if err != nil {
					panic(err)
				}

				messages = nextMessages
				printAnswer(answer)

			case "wake":
				// S16 改动：wake 分支只做非破坏性判断。
				//
				// Lead inbox 必须由 collectInboxNotifications 统一读取并路由 protocol response。
				hasInbox := messageBus != nil && messageBus.Peek("lead")
				hasBackground := bgTracker != nil && bgTracker.HasCompleted()
				if !hasInbox && !hasBackground {
					continue
				}

				fmt.Printf(
					"\n\033[33m[wake: inbox=%t background=%t -> new turn]\033[0m\n",
					hasInbox,
					hasBackground,
				)

				answer, nextMessages, err := runAgentTurn(
					ctx,
					client,
					chatTools,
					toolbox,
					hookBus,
					cronScheduler,
					bgTracker,
					messageBus,
					protocolBook,
					memoryLibrary,
					&promptCache,
					promptSections,
					enabledTools,
					skillRegistry.List(),
					workdir,
					"",
					messages,
				)
				if err != nil {
					panic(err)
				}

				messages = nextMessages
				printAnswer(answer)
			}

			if spawner.HasActive() {
				hadTeammates = true
				continue
			}

			if hadTeammates && !messageBus.Peek("lead") && !bgTracker.HasCompleted() {
				fmt.Println("\033[32m[all teammates done]\033[0m")
				fmt.Println()
				hadTeammates = false
			}
		}
	}
}

// startInputReader 对标 Python input_reader。
//
// 独立 goroutine 只读取终端输入并投递事件，不直接修改 messages。
func startInputReader(
	ctx context.Context,
	reader *bufio.Reader,
	events chan<- leadEvent,
) {
	go func() {
		for {
			fmt.Print("\033[36ms18 >> \033[0m")

			line, err := reader.ReadString('\n')
			if err != nil && strings.TrimSpace(line) == "" {
				sendLeadEvent(ctx, events, leadEvent{kind: "quit"})
				return
			}

			if !sendLeadEvent(ctx, events, leadEvent{
				kind: "user",
				text: line,
			}) {
				return
			}
		}
	}()
}

// startInboxPoller 对标 Python inbox_poller。
//
// 非破坏性检查 Lead inbox 和已完成后台任务，发现可注入内容时投递 wake 事件。
func startInboxPoller(
	ctx context.Context,
	messageBus *team.MessageBus,
	bgTracker *background.Tracker,
	events chan<- leadEvent,
) {
	go func() {
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return

			case <-ticker.C:
				if messageBus.Peek("lead") || bgTracker.HasCompleted() {
					if !sendLeadEvent(ctx, events, leadEvent{kind: "wake"}) {
						return
					}
				}
			}
		}
	}()
}

func sendLeadEvent(
	ctx context.Context,
	events chan<- leadEvent,
	event leadEvent,
) bool {
	select {
	case <-ctx.Done():
		return false
	case events <- event:
		return true
	}
}

func appendUserMessage(
	messages []openai.ChatCompletionMessageParamUnion,
	user string,
) []openai.ChatCompletionMessageParamUnion {
	return append(messages, openai.UserMessage(user))
}

// runAgentTurn 对标 Python agent_loop 被 user/wake 事件调用的一次 Lead turn。
//
// S16 在 S15 串行事件循环基础上继续显式传入 ProtocolBook，便于 inbox 注入时路由协议响应。
func runAgentTurn(
	ctx context.Context,
	client openai.Client,
	toolboxSchema []openai.ChatCompletionToolUnionParam,
	toolbox *v2.ToolBox,
	hookBus *hooks.HookBus,
	cronScheduler *cron.Scheduler,
	bgTracker *background.Tracker,
	messageBus *team.MessageBus,
	protocolBook *team.ProtocolBook,
	library memory.Library,
	promptCache *prompt.CacheV2,
	promptSections []prompt.SectionV2,
	enabledTools []string,
	skillList string,
	workdir string,
	userQuery string,
	messages []openai.ChatCompletionMessageParamUnion,
) (string, []openai.ChatCompletionMessageParamUnion, error) {
	query := strings.TrimSpace(userQuery)

	// S10：每个 Agent 回合开始前从真实状态更新 prompt context。
	// 对标 Python: context = update_context(context, history); system = get_system_prompt(context)
	promptContext, err := prompt.UpdateContext(
		workdir,
		enabledTools,
		skillList,
		library,
	)
	if err != nil {
		fmt.Printf("\033[33m[system prompt context skipped: %v]\033[0m\n", err)
	} else {
		messages = setSystemMessage(messages, promptCache.Get(promptContext, promptSections))
	}

	if query != "" {
		messages = appendUserMessage(messages, query)
	}

	return runAgentLoop(
		ctx,
		client,
		toolboxSchema,
		toolbox,
		hookBus,
		cronScheduler,
		bgTracker,
		messageBus,
		protocolBook,
		library,
		promptCache,
		promptSections,
		enabledTools,
		skillList,
		workdir,
		query,
		messages,
		maxAgentLoopSteps,
	)
}

func runAgentLoop(
	ctx context.Context,
	client openai.Client,
	toolboxSchema []openai.ChatCompletionToolUnionParam,
	toolbox *v2.ToolBox,
	hookBus *hooks.HookBus,
	cronScheduler *cron.Scheduler,
	bgTracker *background.Tracker,
	messageBus *team.MessageBus,
	protocolBook *team.ProtocolBook,
	library memory.Library,
	promptCache *prompt.CacheV2,
	promptSections []prompt.SectionV2,
	enabledTools []string,
	skillList string,
	workdir string,
	currentUserText string,
	messages []openai.ChatCompletionMessageParamUnion,
	maxSteps int,
) (string, []openai.ChatCompletionMessageParamUnion, error) {
	params := openai.ChatCompletionNewParams{
		Model:    modelID,
		Messages: messages,
		Tools:    toolboxSchema,
	}

	toolCallCount := 0
	roundsSinceTodo := 0

	recoveryState := recovery.NewState(modelID, fallbackModelID)
	maxTokens := recovery.DefaultMaxTokens

	messages, _ = collectCronNotifications(messages, cronScheduler)
	// S16 新增：回合开始先消费 Lead inbox，并用 ProtocolBook 路由 response。
	messages, _ = collectInboxNotifications(messages, messageBus, protocolBook)
	messages, _ = collectBackgroundNotifications(messages, bgTracker)

	memoriesContent, err := memory.Load(ctx, client, modelID, library, messages)
	if err != nil {
		fmt.Printf("\033[33m[Memory load skipped: %v]\033[0m\n", err)
		memoriesContent = ""
	}

	for step := 0; step < maxSteps; step++ {
		var err error

		messages, _ = collectCronNotifications(messages, cronScheduler)
		// S16 新增：每轮模型请求前都检查 teammate 消息，避免协议响应滞留。
		messages, _ = collectInboxNotifications(messages, messageBus, protocolBook)
		messages, _ = collectBackgroundNotifications(messages, bgTracker)

		promptContext, err := prompt.UpdateContext(
			workdir,
			enabledTools,
			skillList,
			library,
		)
		if err != nil {
			fmt.Printf("\033[33m[system prompt context skipped: %v]\033[0m\n", err)
		} else {
			messages = setSystemMessage(messages, promptCache.Get(promptContext, promptSections))
		}

		preCompress := openaiadapter.CloneMessages(messages)

		// S08：budget → snip → micro → auto compact。
		messages, err = compact.ToolResultBudget(
			messages,
			workdir,
			200000,
		)
		if err != nil {
			return "", messages, err
		}

		messages = compact.SnipCompact(messages, 50)
		messages = compact.MicroCompact(messages)

		if compact.EstimateSize(messages) > compact.CONTEXT_LIMIT {
			fmt.Println("[auto compact]")

			messages, err = compact.CompactHistory(ctx, client, recoveryState.CurrentModel, workdir, messages)
			if err != nil {
				return "", messages, err
			}
		}

		requestMessages := injectMemoriesIntoCurrentUser(
			messages,
			memoriesContent,
			currentUserText,
		)

		params.Messages = requestMessages

		params.Model = recoveryState.CurrentModel
		params.MaxCompletionTokens = openai.Int(maxTokens)

		completion, err := recovery.WithRetry(
			ctx,
			&recoveryState,
			func(model string) (*openai.ChatCompletion, error) {
				params.Model = model
				return client.Chat.Completions.New(ctx, params)
			},
		)
		if err != nil {
			// S11：prompt too long 只 reactive compact 一次。
			if recovery.IsPromptTooLong(err) {
				if !recoveryState.HasAttemptedReactiveCompact {
					fmt.Println("  \033[31m[reactive compact]\033[0m")

					messages, err = compact.ReactiveCompact(ctx, client, recoveryState.CurrentModel, workdir, messages)
					if err != nil {
						return "", messages, err
					}

					recoveryState.HasAttemptedReactiveCompact = true
					continue
				}

				answer := "[Error] Context too large, cannot continue."

				fmt.Println("\033[31m[unrecoverable] still too long after compact\033[0m")

				messages = append(messages, openai.AssistantMessage(answer))

				return answer, messages, nil
			}

			answer := recovery.ErrorText(err)

			fmt.Printf("  \033[31m[unrecoverable] %s\033[0m\n", answer)

			messages = append(messages, openai.AssistantMessage(answer))

			return answer, messages, nil
		}

		if len(completion.Choices) == 0 {
			answer := "[Error] empty completion choices"
			messages = append(messages, openai.AssistantMessage(answer))

			return answer, messages, nil
		}

		choice := completion.Choices[0]
		msg := choice.Message

		// S11：length → 首次升级 64K，之后最多续写三次。
		if recovery.IsMaxTokensFinishReason(choice.FinishReason) {
			if !recoveryState.HasEscalated {
				fmt.Printf("  \033[33m[max_tokens] escalating %d -> %d\033[0m\n",
					recovery.DefaultMaxTokens,
					recovery.EscalatedMaxTokens,
				)

				maxTokens = recovery.EscalatedMaxTokens
				recoveryState.HasEscalated = true

				continue
			}

			// OpenAI 必要保护：
			// 被截断的 tool call 参数可能不完整，不能直接追加 continuation。
			if len(msg.ToolCalls) > 0 {
				answer := "[Error] Output token limit reached while generating a tool call."
				messages = append(messages, openai.AssistantMessage(answer))

				return answer, messages, nil
			}

			messages = append(messages, msg.ToParam())

			if recoveryState.RecoveryCount < recovery.MaxRecoveryRetries {
				recoveryState.RecoveryCount++

				fmt.Printf("  \033[33m[max_tokens] continuation %d/%d\033[0m\n",
					recoveryState.RecoveryCount,
					recovery.MaxRecoveryRetries,
				)

				messages = append(messages, openai.UserMessage(recovery.ContinuationPrompt))

				continue
			}

			fmt.Println("  \033[31m[max_tokens] recovery limit reached\033[0m")

			return msg.Content, messages, nil
		}

		messages = append(messages, msg.ToParam())

		if len(msg.ToolCalls) == 0 {
			var injected int
			messages, injected = collectCronNotifications(messages, cronScheduler)
			if injected > 0 {
				continue
			}

			// S16 新增：assistant 暂停时如果有协议响应，注入后继续下一轮。
			messages, injected = collectInboxNotifications(messages, messageBus, protocolBook)
			if injected > 0 {
				continue
			}

			messages, injected = collectBackgroundNotifications(messages, bgTracker)
			if injected > 0 {
				continue
			}

			force := hookBus.TriggerStop(ctx, hooks.StopContext{
				MessageCount:  len(messages),
				ToolCallCount: toolCallCount,
			})

			if force != "" {
				messages = append(
					messages,
					openai.UserMessage(force),
				)
				continue
			}

			// 回合结束后提取 memory，并在达到阈值后 consolidate。
			if _, err := memory.Extract(ctx, client, modelID, library, preCompress); err != nil {
				fmt.Printf("\033[33m[Memory extract skipped: %v]\033[0m\n", err)
			}

			if err := memory.Consolidate(ctx, client, modelID, library); err != nil {
				fmt.Printf("\033[33m[Memory consolidate skipped: %v]\033[0m\n", err)
			}

			return msg.Content, messages, nil
		}

		roundsSinceTodo++
		compactCalled := false

		for _, toolCall := range msg.ToolCalls {
			toolCallCount++

			// compact 改写 messages，因此仍需特殊处理。
			if toolCall.Function.Name == compactToolName {
				messages, err = compact.CompactHistory(ctx, client, recoveryState.CurrentModel, workdir, messages)
				if err != nil {
					result := fmt.Sprintf(`{"error": %q}`, err.Error())
					messages = append(messages, openai.ToolMessage(result, toolCall.ID))
					continue
				}

				messages = append(
					messages,
					openai.UserMessage("[Compacted. Conversation history has been summarized.]"),
				)

				roundsSinceTodo = 0
				compactCalled = true
				break
			}

			// S13 的后台任务机制只改变工具执行策略，不改变工具调用协议。
			call := v2.ToolCall{
				Name: toolCall.Function.Name,
				Arguments: json.RawMessage(
					toolCall.Function.Arguments,
				),
			}

			blocked := hookBus.TriggerPreToolUse(ctx, call)
			if blocked != "" {
				fmt.Printf(
					"\033[31m%s\033[0m\n",
					blocked,
				)

				messages = append(
					messages,
					openai.ToolMessage(
						blocked,
						toolCall.ID,
					),
				)

				continue
			}

			if bgTracker != nil && background.ShouldRun(toolCall.Function.Name, call.Arguments) {
				bgCall := call
				task := bgTracker.Start(
					toolCall.ID,
					toolCall.Function.Name,
					call.Arguments,
					func() string {
						return executeToolCall(ctx, toolbox, hookBus, bgCall)
					},
				)

				result := fmt.Sprintf(
					"[Background task %s started] Command: %s. Result will be available when complete.",
					task.ID,
					task.Command,
				)

				messages = append(
					messages,
					openai.ToolMessage(result, toolCall.ID),
				)

				continue
			}

			result := executeToolCall(ctx, toolbox, hookBus, call)

			if toolCall.Function.Name == "todo_write" {
				roundsSinceTodo = 0
			}

			messages = append(
				messages,
				openai.ToolMessage(result, toolCall.ID),
			)
		}

		messages, _ = collectCronNotifications(messages, cronScheduler)
		// S16 新增：工具执行后也统一路由 Lead inbox，尤其是 review_plan 后的后续响应。
		messages, _ = collectInboxNotifications(messages, messageBus, protocolBook)
		messages, _ = collectBackgroundNotifications(messages, bgTracker)

		if compactCalled {
			continue
		}

		if roundsSinceTodo >= 3 {
			messages = append(
				messages,
				openai.UserMessage("<reminder>Update your todos.</reminder>"),
			)

			roundsSinceTodo = 0
		}
	}

	return "", messages, fmt.Errorf("agent loop reached max steps")
}

// executeToolCall 对标 Python execute_tool。
//
// 统一执行一次工具调用，并保留 Go 端已有的错误包装与 PostToolUse hook。
func executeToolCall(
	ctx context.Context,
	toolbox *v2.ToolBox,
	hookBus *hooks.HookBus,
	call v2.ToolCall,
) string {
	result, err := toolbox.Execute(ctx, call)
	if err != nil {
		result = fmt.Sprintf(`{"error": %q}`, err.Error())
	}

	postResult := hookBus.TriggerPostToolUse(ctx, call, result)
	if strings.TrimSpace(postResult) != "" {
		result = postResult
	}

	return result
}

// collectCronNotifications 对标 Python consume_cron_queue 的注入步骤。
//
// OpenAI Chat Completions 仍把触发后的计划任务作为后续 user message 注入真实历史。
func collectCronNotifications(
	messages []openai.ChatCompletionMessageParamUnion,
	cronScheduler *cron.Scheduler,
) ([]openai.ChatCompletionMessageParamUnion, int) {
	if cronScheduler == nil {
		return messages, 0
	}

	fired := cronScheduler.ConsumeQueue()
	if len(fired) == 0 {
		return messages, 0
	}

	for _, job := range fired {
		messages = append(messages, openai.UserMessage("[Scheduled] "+job.Prompt))
		fmt.Printf(
			"  \033[35m[inject cron] %s\033[0m\n",
			previewRunes(job.Prompt, 50),
		)
	}

	return messages, len(fired)
}

// collectInboxNotifications 对标 Python consume_lead_inbox(route_protocol=True)。
//
// Lead inbox 被读取时先路由协议响应，再把消息注入真实历史，避免协议 response 被读走但未匹配。
func collectInboxNotifications(
	messages []openai.ChatCompletionMessageParamUnion,
	messageBus *team.MessageBus,
	protocolBook *team.ProtocolBook,
) ([]openai.ChatCompletionMessageParamUnion, int) {
	if messageBus == nil {
		return messages, 0
	}

	inbox, err := team.ConsumeLeadInbox(messageBus, protocolBook)
	if err != nil {
		fmt.Printf("\033[33m[inbox read skipped: %v]\033[0m\n", err)
		return messages, 0
	}

	if len(inbox) == 0 {
		return messages, 0
	}

	lines := make([]string, 0, len(inbox))

	for _, msg := range inbox {
		reqID := teamMetaRequestID(msg)

		tag := "[" + msg.Type + "]"
		if reqID != "" {
			tag = fmt.Sprintf("[%s req:%s]", msg.Type, reqID)
		}

		lines = append(
			lines,
			fmt.Sprintf(
				"From %s %s: %s",
				msg.From,
				tag,
				previewRunes(msg.Content, 200),
			),
		)
	}

	content := "[Inbox]\n" + strings.Join(lines, "\n")
	messages = append(messages, openai.UserMessage(content))

	fmt.Printf(
		"  \033[33m[inject inbox] %d message(s)\033[0m\n",
		len(inbox),
	)

	return messages, len(inbox)
}

// teamMetaRequestID 对标 Python msg.metadata.get("request_id")。
//
// 只在 S16 main 中格式化 Lead inbox 预览，不承担协议匹配职责。
func teamMetaRequestID(msg team.Message) string {
	return team.MetaString(msg.Metadata, "request_id")
}

// collectBackgroundNotifications 对标 Python collect_background_results 的注入步骤。
//
// OpenAI Chat Completions 需要 tool message 先逐条补齐，因此通知作为后续 user message 追加。
func collectBackgroundNotifications(
	messages []openai.ChatCompletionMessageParamUnion,
	bgTracker *background.Tracker,
) ([]openai.ChatCompletionMessageParamUnion, int) {
	if bgTracker == nil {
		return messages, 0
	}

	notifications := bgTracker.Collect()
	if len(notifications) == 0 {
		return messages, 0
	}

	for _, notification := range notifications {
		messages = append(messages, openai.UserMessage(notification))
	}

	fmt.Printf(
		"  \033[32m[inject] %d background notification(s)\033[0m\n",
		len(notifications),
	)

	return messages, len(notifications)
}

// previewRunes 对标 Python prompt[:50]。
//
// Go 字符串按 byte 切容易截断中文；这里按 rune 截断，只用于终端预览输出。
func previewRunes(text string, limit int) string {
	if limit <= 0 {
		return ""
	}

	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}

	return string(runes[:limit])
}

func printAnswer(answer string) {
	if strings.TrimSpace(answer) == "" {
		return
	}

	fmt.Println(answer)
	fmt.Println()
}

// setSystemMessage 对标 Python system=get_system_prompt(context)。
//
// OpenAI Chat Completions 把 system 放在 messages 中，因此更新首条控制消息。
func setSystemMessage(
	messages []openai.ChatCompletionMessageParamUnion,
	system string,
) []openai.ChatCompletionMessageParamUnion {
	if len(messages) == 0 {
		return []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage(system),
		}
	}
	messages[0] = openai.SystemMessage(system)
	return messages
}

// injectMemoriesIntoCurrentUser 对标 Python request_messages[memory_turn] 注入。
//
// 只修改本次请求副本，不把 relevant memories 永久写入真实消息历史。
func injectMemoriesIntoCurrentUser(
	messages []openai.ChatCompletionMessageParamUnion,
	memoriesContent string,
	currentUserText string,
) []openai.ChatCompletionMessageParamUnion {
	if strings.TrimSpace(memoriesContent) == "" || strings.TrimSpace(currentUserText) == "" {
		return messages
	}

	out := openaiadapter.CloneMessages(messages)

	// Python 版通过 memory_turn 定位当前 user turn。
	// Go 端经过 SnipCompact / CompactHistory 后下标可能变化，
	// 所以从后往前找当前用户原文，只修改 request 副本。
	for i := len(out) - 1; i >= 0; i-- {
		text, ok := openaiadapter.UserTextContent(out[i])
		if !ok || text != currentUserText {
			continue
		}

		out[i] = openai.UserMessage(memoriesContent + "\n\n" + text)

		return out
	}

	return messages
}
