package v3

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
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

// =========================
// Target：Agent Loop 依赖的统一接口
// =========================

type Tool interface {
	Schema() ToolSchema
	Call(ctx context.Context, arguments json.RawMessage) (string, error)
}

// =========================
// Adaptee 抽象：真正执行工具逻辑的对象
// =========================

type ToolInvoker interface {
	Invoke(ctx context.Context, arguments json.RawMessage) (string, error)
}

// InvokerFunc 是对普通 Go 函数的适配前形态。
// 它还不是 Tool，因为它没有 Schema。
type InvokerFunc func(ctx context.Context, arguments json.RawMessage) (string, error)

func (fn InvokerFunc) Invoke(ctx context.Context, arguments json.RawMessage) (string, error) {
	if fn == nil {
		return "", fmt.Errorf("tool invoker function is nil")
	}
	return fn(ctx, arguments)
}

// =========================
// Adapter：把 ToolInvoker 适配成 Tool
// =========================

type ToolAdapter struct {
	schema  ToolSchema
	invoker ToolInvoker
}

func NewToolAdapter(schema ToolSchema, invoker ToolInvoker) (*ToolAdapter, error) {
	if err := validateToolSchema(schema); err != nil {
		return nil, err
	}

	if invoker == nil {
		return nil, fmt.Errorf("tool %q invoker is nil", schema.Name)
	}

	return &ToolAdapter{
		schema:  schema,
		invoker: invoker,
	}, nil
}

func (a *ToolAdapter) Schema() ToolSchema {
	return a.schema
}

func (a *ToolAdapter) Call(ctx context.Context, arguments json.RawMessage) (string, error) {
	return a.invoker.Invoke(ctx, arguments)
}

// =========================
// 本地函数工具：普通函数 -> Tool
// =========================

func NewFunctionTool(
	name string,
	description string,
	parameters map[string]any,
	fn func(ctx context.Context, arguments json.RawMessage) (string, error),
) (*ToolAdapter, error) {
	return NewToolAdapter(
		ToolSchema{
			Name:        name,
			Description: description,
			Parameters:  parameters,
		},
		InvokerFunc(fn),
	)
}

type ToolBox struct {
	tools map[string]Tool
}

func NewToolBox(tools ...Tool) (*ToolBox, error) {
	box := &ToolBox{
		tools: make(map[string]Tool),
	}

	if err := box.RegisterMany(tools...); err != nil {
		return nil, err
	}

	return box, nil
}

func (b *ToolBox) Register(tool Tool) error {
	if tool == nil {
		return fmt.Errorf("tool is nil")
	}

	schema := tool.Schema()
	if err := validateToolSchema(schema); err != nil {
		return err
	}

	if _, exists := b.tools[schema.Name]; exists {
		return fmt.Errorf("tool %q already registered", schema.Name)
	}

	b.tools[schema.Name] = tool
	return nil
}

func (b *ToolBox) RegisterMany(tools ...Tool) error {
	for _, tool := range tools {
		if err := b.Register(tool); err != nil {
			return err
		}
	}
	return nil
}

func (b *ToolBox) Execute(ctx context.Context, call ToolCall) (string, error) {
	name := strings.TrimSpace(call.Name)
	if name == "" {
		return "", fmt.Errorf("tool call name is empty")
	}

	tool, ok := b.tools[name]
	if !ok {
		return "", fmt.Errorf("tool %q not found", name)
	}

	return tool.Call(ctx, call.Arguments)
}

func (b *ToolBox) Schemas() []ToolSchema {
	schemas := make([]ToolSchema, 0, len(b.tools))

	for _, tool := range b.tools {
		schemas = append(schemas, tool.Schema())
	}

	return schemas
}

func validateToolSchema(schema ToolSchema) error {
	if strings.TrimSpace(schema.Name) == "" {
		return fmt.Errorf("tool name is empty")
	}

	if schema.Parameters == nil {
		return fmt.Errorf("tool %q parameters is nil", schema.Name)
	}

	return nil
}
