package main

import (
	"AgentLoop/internal/agentui"
	"AgentLoop/internal/modelclient"
	"AgentLoop/mini_agent_loop/openai_model"
	"AgentLoop/mini_agent_loop/openai_model/tools"
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	v2 "AgentLoop/mini_agent_loop/openai_model/tools/v2"

	"github.com/openai/openai-go/v3"
)

func main() {
	ctx := context.Background()
	client, _, err := modelclient.NewFromEnv(modelclient.Aliyun())
	if err != nil {
		panic(err)
	}

	toolbox := v2.NewToolBox(
		tools.NewWeatherToolV2(),
		tools.NewBashToolV2(),
		tools.NewReadFileToolV2(),
		tools.NewWriteFileToolV2(),
		tools.NewEditFileToolV2(),
	)

	chatTools, err := openai_model.ToChatCompletionToolsV2(toolbox.Schemas())
	if err != nil {
		panic(err)
	}
	reader := bufio.NewReader(os.Stdin)
	permission := openai_model.NewPermissionCheckerWithReader(reader)

	system := "你是一个智能体猫猫娘，拥有 Bash 工具能力，回答时保持可爱但专业的猫猫娘语气，按状态少量使用 Emoji（如 🐾执行中、✅完成、⚠️注意、❌失败、📌总结），能用工具验证就验证，直接给结果，不解释身份设定、不输出内部思考、不啰嗦。"

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

		messages = appendUserMessage(messages, query)
		answer, nextMessages, err := runAgentLoop(ctx, client, chatTools, toolbox, permission, messages, 20)
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
	permission *openai_model.PermissionChecker,
	messages []openai.ChatCompletionMessageParamUnion,
	maxSteps int,
) (string, []openai.ChatCompletionMessageParamUnion, error) {
	params := openai.ChatCompletionNewParams{
		Model:    "deepseek-v4-pro",
		Messages: messages,
		Tools:    toolboxSchema,
	}

	for step := 0; step < maxSteps; step++ {
		completion, err := client.Chat.Completions.New(ctx, params)
		if err != nil {
			return "", messages, err
		}

		msg := completion.Choices[0].Message
		messages = append(messages, msg.ToParam())

		if len(msg.ToolCalls) == 0 {
			return msg.Content, messages, nil
		}

		for _, toolCall := range msg.ToolCalls {
			call := v2.ToolCall{
				Name:      toolCall.Function.Name,
				Arguments: json.RawMessage(toolCall.Function.Arguments),
			}

			agentui.PrintToolCall(call)

			//S03的核心要点，执行前确认-》真正被加进来的东西

			if permission != nil && !permission.CheckPermission(ctx, call) {
				result := "Permission denied."

				fmt.Printf("\033[31m%s\033[0m\n", result)

				// 即使拒绝，也必须返回一个 ToolMessage。
				// 因为模型已经发起了 tool call，后续消息需要给这个 tool_call_id 一个结果。
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

			messages = append(
				messages,
				openai.ToolMessage(result, toolCall.ID),
			)
		}
		params.Messages = messages
	}
	return "", messages, fmt.Errorf("agent loop reached max steps")
}
