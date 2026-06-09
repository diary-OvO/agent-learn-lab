package main

import (
	"AgentLoop/internal/agentconsole"
	"AgentLoop/internal/compact"
	"AgentLoop/internal/hooks"
	"AgentLoop/internal/loopinit"
	"AgentLoop/internal/modelclient"
	"AgentLoop/internal/openaiadapter"
	"AgentLoop/internal/permission"
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
	modelID            = "deepseek-v4-pro"
	compactToolName    = "compact"
	maxReactiveRetries = 1
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

	hookBus := hooks.NewHookBus()
	loopinit.InitS08Hooks(hookBus, checker, workdir)

	skillsDir := filepath.Join(workdir, "skills")
	skillRegistry, err := skills.Scan(skillsDir)
	if err != nil {
		panic(err)
	}

	subToolbox := loopinit.InitS08SubToolbox()

	subAgent, err := subagent.New(client, subToolbox, hookBus)
	if err != nil {
		panic(err)
	}

	toolbox := loopinit.InitS08Toolbox(subAgent, skillRegistry)

	shcemas := append(toolbox.Schemas(), compact.CompactToolSchema())
	chatTools, err := openaiadapter.ToChatCompletionToolsV2(shcemas)
	if err != nil {
		panic(err)
	}

	system := fmt.Sprintf(
		"你是一个智能体猫猫娘，位于当前工作区 %s。"+
			"\n\n可用 Skills：\n%s\n\n"+
			"当任务需要某个 Skill 的完整说明时，使用 load_skill 工具按 name 加载完整 SKILL.md。"+
			"不要把完整 Skill 内容提前假设进回答；需要时再加载。"+
			"在开始任何多步骤任务前，必须先使用 todo_write 规划步骤。"+
			"遇到复杂子问题、需要上下文隔离或独立调查时，优先使用 task 工具启动子智能体，并只接收其最终结论。"+
			"执行过程中持续更新 todo_write 的状态：开始做某一步前标记为 in_progress，完成后标记为 completed。"+
			"你可以使用 Bash 和文件工具完成任务。"+
			"所有破坏性操作都需要用户批准。"+
			"回答时保持可爱但专业的猫猫娘语气，按状态少量使用 Emoji（如 🐾执行中、✅完成、⚠️注意、❌失败、📌总结）。"+
			"能用工具验证就验证，直接给结果，不解释身份设定、不输出内部思考、不啰嗦。",
		workdir,
		skillRegistry.List(),
	)

	messages := []openai.ChatCompletionMessageParamUnion{
		openai.SystemMessage(system),
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

		messages = appendUserMessage(messages, query)
		answer, nextMessages, err := runAgentLoop(ctx, client, chatTools, toolbox, hookBus, workdir, messages, 20)
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
	workdir string,
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
	reactiveRetries := 0
	for step := 0; step < maxSteps; step++ {
		//S08 执行运行前的压缩检测
		var err error
		messages, err = compact.ToolResultBudget(messages, workdir, 200000)
		if err != nil {
			return "", messages, err
		}
		messages = compact.SnipCompact(messages, 50)
		messages = compact.MicroCompact(messages)
		if compact.EstimateSize(messages) > compact.CONTEXT_LIMIT {
			fmt.Println("[auto compact]")

			messages, err = compact.CompactHistory(ctx, client, modelID, workdir, messages)
			if err != nil {
				return "", messages, err
			}
		}
		params.Messages = messages
		completion, err := client.Chat.Completions.New(ctx, params)
		if err != nil {
			//  报错捕获，针对上下文过长报错进行强制压缩
			if shouldReactiveCompact(err) && reactiveRetries < maxReactiveRetries {
				fmt.Println("[reactive compact]")

				messages, err = compact.ReactiveCompact(ctx, client, modelID, workdir, messages)
				if err != nil {
					return "", messages, err
				}

				reactiveRetries++
				params.Messages = messages
				continue
			}
			return "", messages, err
		}

		reactiveRetries = 0

		msg := completion.Choices[0].Message
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

			return msg.Content, messages, nil
		}
		roundsSinceTodo++
		compactCalled := false

		for _, toolCall := range msg.ToolCalls {
			toolCallCount++

			if toolCall.Function.Name == compactToolName {
				messages, err = compact.CompactHistory(ctx, client, modelID, workdir, messages)
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
func shouldReactiveCompact(err error) bool {
	if err == nil {
		return false
	}

	s := strings.ToLower(err.Error())

	return strings.Contains(s, "prompt_too_long") ||
		strings.Contains(s, "too many tokens") ||
		strings.Contains(s, "context length exceeded") ||
		strings.Contains(s, "maximum context length") ||
		strings.Contains(s, "tokens exceed")
}
