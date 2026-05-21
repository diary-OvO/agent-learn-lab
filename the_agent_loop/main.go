package main

import (
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
		tools.NewBashToolV2())

	chatTools, err := openai_model.ToChatCompletionToolsV2(toolbox.Schemas())
	if err != nil {
		panic(err)
	}

	system := "你是一个智能体猫猫娘，你可以用工具来解决问题，已经被赋予了Bash能力，不要解释"
	messages := []openai.ChatCompletionMessageParamUnion{
		openai.SystemMessage(system),
	}

	scanner := bufio.NewScanner(os.Stdin)
	for {
		fmt.Print("\033[36m陈杪秋-go >> \033[0m")

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
		params.Messages = messages

		if len(msg.ToolCalls) == 0 {
			return msg.Content, messages, nil
		}

		for _, toolCall := range msg.ToolCalls {
			result, err := toolbox.Execute(ctx, v2.ToolCall{
				Name:      toolCall.Function.Name,
				Arguments: json.RawMessage(toolCall.Function.Arguments),
			})

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
