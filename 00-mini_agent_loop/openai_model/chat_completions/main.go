package main

import (
	"AgentLoop/00-mini_agent_loop/openai_model"
	"AgentLoop/00-mini_agent_loop/openai_model/tools"
	v1 "AgentLoop/00-mini_agent_loop/openai_model/tools/v1"
	"AgentLoop/internal/modelclient"
	"context"
	"encoding/json"
	"fmt"

	"github.com/openai/openai-go/v3"
)

func main() {
	client, _, err := modelclient.NewFromEnv(modelclient.Aliyun())
	if err != nil {
		panic(err)
	}

	//先注册工具运行的box
	toolbox := v1.NewToolBox(
		tools.NewWeatherToolV1())

	chatTools, err := openai_model.ToChatCompletionTools(toolbox.Schemas())
	if err != nil {
		panic(err)
	}

	params := openai.ChatCompletionNewParams{
		Model: "deepseek-v4-pro",
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage("请使用工具解决问题"),
			openai.UserMessage("今天广州的天气怎么样"),
		},
		Tools: chatTools,
	}

	answer, err := runChatCompletionLoop(context.Background(), client, toolbox, params, 5)
	if err != nil {
		panic(err)
	}
	fmt.Println(answer)
}
func runChatCompletionLoop(
	ctx context.Context,
	client openai.Client,
	toolbox *v1.ToolBox,
	params openai.ChatCompletionNewParams,
	maxSteps int,
) (string, error) {
	for step := 0; step < maxSteps; step++ {
		completion, err := client.Chat.Completions.New(ctx, params)
		if err != nil {
			return "", err
		}

		msg := completion.Choices[0].Message

		if len(msg.ToolCalls) == 0 {
			return msg.Content, nil
		}

		params.Messages = append(params.Messages, msg.ToParam())

		for _, toolCall := range msg.ToolCalls {
			result, err := toolbox.Execute(ctx, v1.ToolCall{
				Name:      toolCall.Function.Name,
				Arguments: json.RawMessage(toolCall.Function.Arguments),
			})

			if err != nil {
				result = fmt.Sprintf(`{"error": %q}`, err.Error())
			}

			params.Messages = append(
				params.Messages,
				openai.ToolMessage(result, toolCall.ID),
			)
		}
	}
	return "", fmt.Errorf("agent loop reached max steps")
}
