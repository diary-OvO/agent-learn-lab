package main

import (
	"AgentLoop/internal/modelclient"
	"AgentLoop/mini_agent_loop/openai_model"
	"AgentLoop/mini_agent_loop/openai_model/tools"
	v1 "AgentLoop/mini_agent_loop/openai_model/tools/v1"
	"context"
	"encoding/json"
	"fmt"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/responses"
)

func main() {
	client, _, err := modelclient.NewFromEnv(modelclient.Aliyun())
	if err != nil {
		panic(err)
	}
	//先注册工具运行的box
	toolbox := v1.NewToolBox(
		tools.NewWeatherToolV1())

	responseTools, err := openai_model.ToResponseTools(toolbox.Schemas())
	if err != nil {
		panic(err)
	}

	params := responses.ResponseNewParams{
		Model: "qwen3.6-plus",
		Input: responses.ResponseNewParamsInputUnion{
			OfString: openai.String("今天北京天气怎么样"),
		},
		Tools: responseTools,
	}

	answer, err := runResponsesLoop(context.Background(), client, toolbox, params, 5)
	if err != nil {
		panic(err)
	}
	fmt.Println(answer)
}
func runResponsesLoop(
	ctx context.Context,
	client openai.Client,
	toolbox *v1.ToolBox,
	params responses.ResponseNewParams,
	maxSteps int,
) (string, error) {
	var previousResponseID string

	for step := 0; step < maxSteps; step++ {
		if previousResponseID != "" {
			params.PreviousResponseID = openai.String(previousResponseID)
		}

		resp, err := client.Responses.New(ctx, params)
		if err != nil {
			return "", err
		}

		previousResponseID = resp.ID

		var toolOutputs []responses.ResponseInputItemUnionParam

		for _, item := range resp.Output {
			if item.Type != "function_call" {
				continue
			}

			toolCall := item.AsFunctionCall()

			result, err := toolbox.Execute(ctx, v1.ToolCall{
				Name:      item.Name,
				Arguments: json.RawMessage(toolCall.Arguments),
			})

			if err != nil {
				result = fmt.Sprintf(`{"error": %q}`, err.Error())
			}
			toolOutputs = append(toolOutputs, responses.ResponseInputItemUnionParam{
				OfFunctionCallOutput: &responses.ResponseInputItemFunctionCallOutputParam{
					CallID: toolCall.CallID,
					Output: responses.ResponseInputItemFunctionCallOutputOutputUnionParam{
						OfString: openai.String(result),
					},
				},
			})
		}

		if len(toolOutputs) == 0 {
			return resp.OutputText(), nil
		}

		params.Input = responses.ResponseNewParamsInputUnion{
			OfInputItemList: toolOutputs,
		}
	}
	return "", fmt.Errorf("agent loop reached max steps")
}
