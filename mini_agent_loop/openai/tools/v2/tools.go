package v2

import (
	"context"
	"encoding/json"
	"fmt"
)

type ToolSchema struct {
	Name        string
	Description string
	Parameters  map[string]any
}

type ToolCall struct {
	Name      string
	Arguments json.RawMessage
}

// Tool interface 写法：
// Agent loop 不关心具体工具是什么结构体。
// 只关心它能不能：
// 1. 返回 Schema
// 2. 执行 Call
type Tool interface {
	Schema() ToolSchema
	Call(ctx context.Context, arguments json.RawMessage) (string, error)
}

type ToolBox struct {
	tools map[string]Tool
}

func NewToolBox(tools ...Tool) *ToolBox {
	box := &ToolBox{
		tools: make(map[string]Tool),
	}

	for _, tool := range tools {
		box.tools[tool.Schema().Name] = tool
	}
	return box
}

func (b *ToolBox) Execute(ctx context.Context, call ToolCall) (string, error) {
	tool, ok := b.tools[call.Name]
	if !ok {
		return "", fmt.Errorf("Tool %s not found", call.Name)
	}
	return tool.Call(ctx, call.Arguments)
}

// FunctionTool 是 Tool interface 的一种实现。
// 能够将一个go的函数包装成一个tool
type FunctionTool struct {
	schema ToolSchema
	fn     func(ctx context.Context, arguments json.RawMessage) (string, error)
}

func NewFunctionTool(
	name string,
	description string,
	parameters map[string]any,
	fn func(ctx context.Context, arguments json.RawMessage) (string, error),
) *FunctionTool {
	return &FunctionTool{
		schema: ToolSchema{
			Name:        name,
			Description: description,
			Parameters:  parameters,
		},
		fn: fn,
	}
}

func (f *FunctionTool) Schema() ToolSchema {
	return f.schema
}
func (f *FunctionTool) Call(ctx context.Context, arguments json.RawMessage) (string, error) {
	return f.fn(ctx, arguments)
}
