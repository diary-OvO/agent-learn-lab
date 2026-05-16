package v1

import (
	"context"
	"encoding/json"
	"fmt"
)

/*
这是一个最基础的最小实现，但是拓展功能比较少
*/
type ToolSchema struct {
	Name        string
	Description string
	Parameters  map[string]any
}

type ToolCall struct {
	Name      string
	Arguments json.RawMessage
}

// Tool struct 写法：
// 一个 Tool 结构体里同时包含：
// 1. 工具声明 Schema
// 2. 工具执行函数 Execute
type Tool struct {
	Schema  ToolSchema
	Execute func(ctx context.Context, arguments json.RawMessage) (string, error)
}

type ToolBox struct {
	tools map[string]Tool
}

func NewToolBox(tools ...Tool) *ToolBox {
	box := &ToolBox{
		tools: make(map[string]Tool),
	}

	for _, tool := range tools {
		box.tools[tool.Schema.Name] = tool
	}

	return box
}

func (b *ToolBox) Execute(ctx context.Context, call ToolCall) (string, error) {
	tool, ok := b.tools[call.Name]
	if !ok {
		return "", fmt.Errorf("tool not found: %s", call.Name)
	}

	return tool.Execute(ctx, call.Arguments)
}
func (b *ToolBox) Schemas() []ToolSchema {
	if b == nil || len(b.tools) == 0 {
		return nil
	}

	out := make([]ToolSchema, 0, len(b.tools))
	for _, tool := range b.tools {
		out = append(out, tool.Schema)
	}

	return out
}

type WeatherArgs struct {
	Location string `json:"location"`
}

func NewWeatherTool() Tool {
	return Tool{
		Schema: ToolSchema{
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
		Execute: func(ctx context.Context, arguments json.RawMessage) (string, error) {
			_ = ctx

			var args WeatherArgs
			if err := json.Unmarshal(arguments, &args); err != nil {
				return "", err
			}

			result := map[string]any{
				"location": args.Location,
				"weather":  "sunny",
			}

			data, err := json.Marshal(result)
			if err != nil {
				return "", err
			}

			return string(data), nil
		},
	}
}
