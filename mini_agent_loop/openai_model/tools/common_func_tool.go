package tools

import (
	v1 "AgentLoop/mini_agent_loop/openai_model/tools/v1"
	v2 "AgentLoop/mini_agent_loop/openai_model/tools/v2"
	"context"
	"encoding/json"
	"fmt"
)

/*
这里就是放一些需要被用上的通用go函数，以及其被注册的注册过程，包含v1/v2两种实现
*/

//----这是一个天气的获取Demo,表现了一个最小实现以及用上interface的一个最小思路-----

type WeatherArgs struct {
	Location string `json:"location"`
}

func getWeather(location string) string {
	return fmt.Sprintf(
		`{"location": %q, "weather": "Sunny", "temperature": "25°C"}`,
		location,
	)
}
func executeGetWeather(ctx context.Context, arguments json.RawMessage) (string, error) {
	var args WeatherArgs
	if err := json.Unmarshal(arguments, &args); err != nil {
		return "", err
	}
	return getWeather(args.Location), nil
}
func NewWeatherToolV1() v1.Tool {
	return v1.Tool{
		Schema: v1.ToolSchema{
			Name:        "get_weather",
			Description: "Get weather at the given location.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"location": map[string]any{
						"type":        "string",
						"description": "City name, e.g. New York City",
					},
				},
				"required":             []string{"location"},
				"additionalProperties": false,
			},
		},
		Execute: executeGetWeather,
	}
}
func NewWeatherToolV2() v2.Tool {
	return v2.NewFunctionTool(
		"get_weather",
		"Get weather at the given location.",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"location": map[string]any{
					"type":        "string",
					"description": "City name, e.g. New York City",
				},
			},
			"required":             []string{"location"},
			"additionalProperties": false,
		},
		executeGetWeather,
	)
}
