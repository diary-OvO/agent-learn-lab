package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"AgentLoop/internal/mcp"
	v2 "AgentLoop/internal/toolkit/v2"
)

type ConnectMCPArgs struct {
	Name string `json:"name"`
}

// NewConnectMCPToolV2 对标 Python run_connect_mcp / connect_mcp tool schema。
//
// 迭代原因：S19 新增 MCP server 连接入口；连接成功后动态 MCP tools 不在这里注册，
// 而是在 main.go 每轮 assembleOpenAIToolPool 时追加，保证旧固定 toolbox 不受影响。
//
// 与普通 tools 差别：这个工具只改变 mcp.Registry 状态，本身不是 MCP 动态工具。
func NewConnectMCPToolV2(registry *mcp.Registry) v2.Tool {
	return v2.NewFunctionTool(
		"connect_mcp",
		"Connect to a mock MCP server and discover its tools.",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"name": map[string]any{
					"type":        "string",
					"description": "MCP server name. Available teaching servers: docs, deploy.",
				},
			},
			"required":             []string{"name"},
			"additionalProperties": false,
		},
		executeConnectMCP(registry),
	)
}

// executeConnectMCP 对标 Python run_connect_mcp。
//
// 解析 server name，并调用 MCP Registry 连接 mock server。
func executeConnectMCP(
	registry *mcp.Registry,
) func(context.Context, json.RawMessage) (string, error) {
	return func(_ context.Context, arguments json.RawMessage) (string, error) {
		if registry == nil {
			return "", fmt.Errorf("mcp registry is nil")
		}

		var args ConnectMCPArgs
		if err := json.Unmarshal(arguments, &args); err != nil {
			return "", err
		}

		args.Name = strings.TrimSpace(args.Name)
		if args.Name == "" {
			return "", fmt.Errorf("name is required")
		}

		return registry.Connect(args.Name)
	}
}
