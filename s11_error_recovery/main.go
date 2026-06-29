package main

import (
	"AgentLoop/internal/agentconsole"
	"AgentLoop/internal/compact"
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
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	v2 "AgentLoop/internal/toolkit/v2"

	"github.com/openai/openai-go/v3"
)

const (
	modelID          = "deepseek-v4-pro"
	fallback_modelID = "qwen3.7-plus"
	compactToolName  = "compact"
)

func main() {
	ctx := context.Background()
	client, _, err := modelclient.NewFromEnv(modelclient.Aliyun())
	if err != nil {
		panic(err)
	}
	ctx = agentconsole.WithAgentScope(ctx, agentconsole.AgentScope{
		Name:  "main",
		ID:    "parent",
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

	hookBus := hooks.NewHookBus()
	loopinit.InitS11Hooks(hookBus, checker, workdir)

	skillsDir := filepath.Join(workdir, "skills")
	skillRegistry, err := skills.Scan(skillsDir)
	if err != nil {
		panic(err)
	}

	subToolbox := loopinit.InitS11SubToolbox()

	subAgent, err := subagent.New(client, subToolbox, hookBus)
	if err != nil {
		panic(err)
	}

	toolbox := loopinit.InitS11Toolbox(subAgent, skillRegistry)

	schemas := append(toolbox.Schemas(), compact.CompactToolSchema())
	chatTools, err := openaiadapter.ToChatCompletionToolsV2(schemas)
	if err != nil {
		panic(err)
	}
	enabledTools := v2.SchemaNames(schemas)
	var promptCache prompt.Cache

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
		openai.SystemMessage(promptCache.Get(promptContext)),
	}

	for {
		fmt.Print("\033[36m喵喵-go >> \033[0m")

		line, err := reader.ReadString('\n')
		if err != nil && strings.TrimSpace(line) == "" {
			break
		}

		query := strings.TrimSpace(line)
		if query == "" ||
			strings.EqualFold(query, "q") ||
			strings.EqualFold(query, "quit") ||
			strings.EqualFold(query, "exit") {
			break
		}
		//输入前注入
		hookedQuery := hookBus.TriggerUserPromptSubmit(ctx, query)
		if strings.TrimSpace(hookedQuery) != "" {
			query = hookedQuery
		}
		// S10：每个用户回合开始前从真实状态更新 prompt context。
		// 对标 Python: context = update_context(context, history); system = get_system_prompt(context)
		promptContext, err = prompt.UpdateContext(
			workdir,
			enabledTools,
			skillRegistry.List(),
			memoryLibrary,
		)
		if err != nil {
			fmt.Printf("\033[33m[system prompt context skipped: %v]\033[0m\n", err)
		} else {
			messages = setSystemMessage(messages, promptCache.Get(promptContext))
		}
		messages = appendUserMessage(messages, query)
		answer, nextMessages, err := runAgentLoop(
			ctx,
			client,
			chatTools,
			toolbox,
			hookBus,
			memoryLibrary,
			&promptCache,
			enabledTools,
			skillRegistry.List(),
			workdir,
			query,
			messages,
			20,
		)
		if err != nil {
			panic(err)
		}

		messages = nextMessages

		fmt.Println(answer)
		fmt.Println()
	}
}

func appendUserMessage(
	messages []openai.ChatCompletionMessageParamUnion,
	user string,
) []openai.ChatCompletionMessageParamUnion {
	return append(messages, openai.UserMessage(user))
}

func runAgentLoop(
	ctx context.Context,
	client openai.Client,
	toolboxSchema []openai.ChatCompletionToolUnionParam,
	toolbox *v2.ToolBox,
	hookBus *hooks.HookBus,
	library memory.Library,
	promptCache *prompt.Cache,
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
	//新增工具调用次数统计
	toolCallCount := 0
	roundsSinceTodo := 0

	recoveryState := recovery.NewState(modelID, fallback_modelID)
	maxTokens := recovery.DefaultMaxTokens

	memoriesContent, err := memory.Load(ctx, client, modelID, library, messages)
	if err != nil {
		fmt.Printf("\033[33m[Memory load skipped: %v]\033[0m\n", err)
		memoriesContent = ""
	}

	for step := 0; step < maxSteps; step++ {
		//S08 执行运行前的压缩检测
		var err error

		promptContext, err := prompt.UpdateContext(
			workdir,
			enabledTools,
			skillList,
			library,
		)
		if err != nil {
			fmt.Printf("\033[33m[system prompt context skipped: %v]\033[0m\n", err)
		} else {
			messages = setSystemMessage(messages, promptCache.Get(promptContext))
		}

		preCompress := openaiadapter.CloneMessages(messages)

		messages, err = compact.ToolResultBudget(messages, workdir, 200000)
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
			// S11 Path 2：prompt_too_long -> reactive compact -> retry once。
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
				fmt.Printf("  \033[31m[unrecoverable] still too long after compact\033[0m\n")

				messages = append(messages, openai.AssistantMessage(answer))

				return answer, messages, nil
			}

			// S11：不可恢复错误不 panic，写入 assistant error 后结束当前 turn。
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
			//结束前注入
			force := hookBus.TriggerStop(ctx, hooks.StopContext{
				MessageCount:  len(messages),
				ToolCallCount: toolCallCount,
			})

			// 如果 Stop hook 返回非空内容，可以把它作为 user message 继续送回模型。
			// 默认 SummaryHook 返回空字符串，所以一般会直接退出。
			if force != "" {
				messages = append(messages, openai.UserMessage(force))
				params.Messages = messages
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

			call := v2.ToolCall{
				Name:      toolCall.Function.Name,
				Arguments: json.RawMessage(toolCall.Function.Arguments),
			}
			//工具执行前注入
			blocked := hookBus.TriggerPreToolUse(ctx, call)
			if blocked != "" {
				result := blocked

				fmt.Printf("\033[31m%s\033[0m\n", result)

				messages = append(
					messages,
					openai.ToolMessage(result, toolCall.ID),
				)

				continue
			}

			result, err := toolbox.Execute(ctx, call)

			if err != nil {
				result = fmt.Sprintf(`{"error": %q}`, err.Error())
			}
			//工具结束前注入
			postResult := hookBus.TriggerPostToolUse(ctx, call, result)
			if strings.TrimSpace(postResult) != "" {
				result = postResult
			}

			if toolCall.Function.Name == "todo_write" {
				roundsSinceTodo = 0
			}
			messages = append(
				messages,
				openai.ToolMessage(result, toolCall.ID),
			)
		}
		if compactCalled {
			params.Messages = messages
			continue
		}
		//加入，Agent忽视了ToDo并且多次都没有调用的话，新增一个执行
		if roundsSinceTodo >= 3 {
			messages = append(
				messages,
				openai.UserMessage("<reminder>Update your todos.</reminder>"),
			)
			roundsSinceTodo = 0
		}
		params.Messages = messages
	}
	return "", messages, fmt.Errorf("agent loop reached max steps")
}
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
