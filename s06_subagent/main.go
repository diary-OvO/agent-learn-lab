package main

import (
	"AgentLoop/internal/agentconsole"
	"AgentLoop/internal/hookimpl"
	"AgentLoop/internal/hooks"
	"AgentLoop/internal/modelclient"
	"AgentLoop/internal/openaiadapter"
	"AgentLoop/internal/permission"
	"AgentLoop/internal/subagent"
	"AgentLoop/internal/tools"
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	v2 "AgentLoop/internal/toolkit/v2"

	"github.com/openai/openai-go/v3"
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
	hookimpl.RegisterS06DefaultHooks(hookBus, checker, workdir)

	subToolbox := v2.NewToolBox(
		tools.NewWeatherToolV2(),
		tools.NewBashToolV2(),
		tools.NewReadFileToolV2(),
		tools.NewWriteFileToolV2(),
		tools.NewEditFileToolV2(),
		tools.NewGlobToolV2(),
	)

	subagent, err := subagent.New(client, subToolbox, hookBus)
	if err != nil {
		panic(err)
	}

	toolbox := v2.NewToolBox(
		tools.NewWeatherToolV2(),
		tools.NewBashToolV2(),
		tools.NewReadFileToolV2(),
		tools.NewWriteFileToolV2(),
		tools.NewEditFileToolV2(),
		tools.NewGlobToolV2(),
		tools.NewTodoWriteToolV2(),
		tools.NewTaskToolV2(subagent),
	)

	chatTools, err := openaiadapter.ToChatCompletionToolsV2(toolbox.Schemas())
	if err != nil {
		panic(err)
	}

	system := fmt.Sprintf(
		"你是一个智能体猫猫娘，位于当前工作区 %s。"+
			"在开始任何多步骤任务前，必须先使用 todo_write 规划步骤。"+
			"执行过程中持续更新 todo_write 的状态：开始做某一步前标记为 in_progress，完成后标记为 completed。"+
			"你可以使用 Bash 和文件工具完成任务。"+
			"所有破坏性操作都需要用户批准。"+
			"回答时保持可爱但专业的猫猫娘语气，按状态少量使用 Emoji（如 🐾执行中、✅完成、⚠️注意、❌失败、📌总结）。"+
			"能用工具验证就验证，直接给结果，不解释身份设定、不输出内部思考、不啰嗦。",
		workdir,
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
		_ = hookBus.TriggerUserPromptSubmit(ctx, query)

		messages = appendUserMessage(messages, query)
		answer, nextMessages, err := runAgentLoop(ctx, client, chatTools, toolbox, hookBus, messages, 20)
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
	messages []openai.ChatCompletionMessageParamUnion,
	maxSteps int,
) (string, []openai.ChatCompletionMessageParamUnion, error) {
	params := openai.ChatCompletionNewParams{
		Model:    "deepseek-v4-pro",
		Messages: messages,
		Tools:    toolboxSchema,
	}
	//新增工具调用次数统计
	toolCallCount := 0
	roundsSinceTodo := 0

	for step := 0; step < maxSteps; step++ {

		completion, err := client.Chat.Completions.New(ctx, params)
		if err != nil {
			return "", messages, err
		}

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
		for _, toolCall := range msg.ToolCalls {
			call := v2.ToolCall{
				Name:      toolCall.Function.Name,
				Arguments: json.RawMessage(toolCall.Function.Arguments),
			}
			toolCallCount++
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
			_ = hookBus.TriggerPostToolUse(ctx, call, result)

			if toolCall.Function.Name == "todo_write" {
				roundsSinceTodo = 0
			}
			messages = append(
				messages,
				openai.ToolMessage(result, toolCall.ID),
			)
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
