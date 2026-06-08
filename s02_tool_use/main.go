package main

import (
	"AgentLoop/internal/agentconsole"
	"AgentLoop/internal/modelclient"
	"AgentLoop/internal/openaiadapter"
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

const modelID = "deepseek-v4-pro"

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

	chatTools, err := openaiadapter.ToChatCompletionToolsV2(toolbox.Schemas())
	if err != nil {
		panic(err)
	}

	system := "你是一个智能体猫猫娘，拥有 Bash 工具能力，回答时保持可爱但专业的猫猫娘语气，按状态少量使用 Emoji（如 🐾执行中、✅完成、⚠️注意、❌失败、📌总结），能用工具验证就验证，直接给结果，不解释身份设定、不输出内部思考、不啰嗦。"
	messages := []openai.ChatCompletionMessageParamUnion{
		openai.SystemMessage(system),
	}

	scanner := bufio.NewScanner(os.Stdin)
	for {
		fmt.Print("\033[36m喵喵-go >> \033[0m")

		if !scanner.Scan() {
			break
		}

		query := strings.TrimSpace(scanner.Text())
		if query == "" || strings.EqualFold(query, "q") || strings.EqualFold(query, "quit") || strings.EqualFold(query, "exit") {
			break
		}

		messages = appendUserMessage(messages, query)
		answer, nextMessages, err := runAgentLoop(ctx, client, chatTools, toolbox, messages, 20)
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
	messages []openai.ChatCompletionMessageParamUnion,
	maxSteps int,
) (string, []openai.ChatCompletionMessageParamUnion, error) {
	params := openai.ChatCompletionNewParams{
		Model:    modelID,
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
		params.Messages = messages

		if len(msg.ToolCalls) == 0 {
			return msg.Content, messages, nil
		}

		for _, toolCall := range msg.ToolCalls {
			call := v2.ToolCall{
				Name:      toolCall.Function.Name,
				Arguments: json.RawMessage(toolCall.Function.Arguments),
			}

			agentconsole.PrintToolCall(ctx, call)

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
