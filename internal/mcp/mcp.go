package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"

	v2 "AgentLoop/internal/toolkit/v2"
)

// Handler 对标 Python MCPClient._handlers。
//
// 教学版 MCP server 不接真实 transport，而是用 Go 函数模拟远端 tool handler。
type Handler func(args map[string]any) (string, error)

// ToolDefinition 对标 Python MCPClient.tools 中的一条 tool_def。
//
// 迭代原因：S19 需要把 MCP server 发现到的 inputSchema 转成项目内的 v2.ToolSchema。
// 与普通内置工具差别：这里记录的是 MCP 原始工具名，暴露给模型时会加 mcp__{server}__{tool} 前缀。
type ToolDefinition struct {
	Name        string
	Description string
	InputSchema map[string]any
	ReadOnly    bool
	Destructive bool
}

// Client 对标 Python MCPClient。
//
// 它只保存一个 mock MCP server 暴露的工具定义和 handler，不负责 Agent Loop。
type Client struct {
	Name     string
	tools    []ToolDefinition
	handlers map[string]Handler
}

// NewClient 对标 Python MCPClient(name)。
//
// 创建一个教学版 MCP client 状态容器。
func NewClient(name string) *Client {
	return &Client{
		Name:     strings.TrimSpace(name),
		tools:    make([]ToolDefinition, 0),
		handlers: make(map[string]Handler),
	}
}

// Register 对标 Python MCPClient.register。
//
// 注册 mock MCP server 暴露的工具定义和对应 handler。
func (c *Client) Register(
	toolDefs []ToolDefinition,
	handlers map[string]Handler,
) {
	if c == nil {
		return
	}

	c.tools = append([]ToolDefinition(nil), toolDefs...)
	c.handlers = make(map[string]Handler, len(handlers))

	for name, handler := range handlers {
		c.handlers[name] = handler
	}
}

// Tools 对标 Python MCPClient.tools。
//
// 返回工具定义副本，避免外部修改 client 内部状态。
func (c *Client) Tools() []ToolDefinition {
	if c == nil {
		return nil
	}

	return append([]ToolDefinition(nil), c.tools...)
}

// CallTool 对标 Python MCPClient.call_tool。
//
// 调用 mock handler；教学版把 handler 错误转成普通工具文本交回模型。
func (c *Client) CallTool(
	toolName string,
	args map[string]any,
) string {
	if c == nil {
		return "MCP error: client is nil"
	}

	handler, ok := c.handlers[toolName]
	if !ok {
		return fmt.Sprintf("MCP error: unknown tool %q", toolName)
	}

	out, err := handler(args)
	if err != nil {
		return "MCP error: " + err.Error()
	}

	return out
}

// Registry 对标 Python mcp_clients + MOCK_SERVERS。
//
// 迭代原因：S19 需要在运行中连接 MCP server，并把已连接 server 的工具动态并入 tool pool。
// 与 S18 固定 toolbox 差别：Registry 只承载动态 MCP 状态，不替代原有 ToolBox。
type Registry struct {
	mu        sync.Mutex
	clients   map[string]*Client
	factories map[string]func() *Client
}

// NewRegistry 对标 Python MOCK_SERVERS 初始化。
//
// 创建包含 docs/deploy 两个教学版 mock MCP server 的注册表。
func NewRegistry() *Registry {
	return &Registry{
		clients: make(map[string]*Client),
		factories: map[string]func() *Client{
			"docs":   mockServerDocs,
			"deploy": mockServerDeploy,
		},
	}
}

// Connect 对标 Python connect_mcp。
//
// 连接一个 mock MCP server，发现其工具，并让后续 assemble tool pool 能看到这些工具。
func (r *Registry) Connect(name string) (string, error) {
	if r == nil {
		return "", fmt.Errorf("mcp registry is nil")
	}

	name = strings.TrimSpace(name)
	if name == "" {
		return "", fmt.Errorf("server name is required")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.clients[name]; ok {
		return fmt.Sprintf("MCP server %q already connected", name), nil
	}

	factory, ok := r.factories[name]
	if !ok {
		available := make([]string, 0, len(r.factories))
		for serverName := range r.factories {
			available = append(available, serverName)
		}
		sort.Strings(available)

		return fmt.Sprintf(
			"Unknown server %q. Available: %s",
			name,
			strings.Join(available, ", "),
		), nil
	}

	client := factory()
	r.clients[name] = client

	toolNames := make([]string, 0, len(client.tools))
	for _, tool := range client.tools {
		toolNames = append(toolNames, tool.Name)
	}
	sort.Strings(toolNames)

	fmt.Printf(
		"  \033[31m[mcp] connected: %s -> %v\033[0m\n",
		name,
		toolNames,
	)

	return fmt.Sprintf(
		"Connected to MCP server %q. Discovered %d tools: %s",
		name,
		len(toolNames),
		strings.Join(toolNames, ", "),
	), nil
}

// ConnectedNames 对标 Python list(mcp_clients.keys())。
//
// 返回当前已连接 MCP server 名称，供 prompt 或调试展示。
func (r *Registry) ConnectedNames() []string {
	if r == nil {
		return nil
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	names := make([]string, 0, len(r.clients))
	for name := range r.clients {
		names = append(names, name)
	}
	sort.Strings(names)

	return names
}

// ToolSchemas 对标 Python assemble_tool_pool 中追加 MCP tools。
//
// 把所有已连接 MCP server 的工具转换为 v2.ToolSchema，名称变成 mcp__{server}__{tool}。
func (r *Registry) ToolSchemas() []v2.ToolSchema {
	if r == nil {
		return nil
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	serverNames := make([]string, 0, len(r.clients))
	for name := range r.clients {
		serverNames = append(serverNames, name)
	}
	sort.Strings(serverNames)

	schemas := make([]v2.ToolSchema, 0)

	for _, serverName := range serverNames {
		client := r.clients[serverName]
		safeServer := NormalizeName(serverName)

		tools := append([]ToolDefinition(nil), client.tools...)
		sort.Slice(tools, func(i, j int) bool {
			return tools[i].Name < tools[j].Name
		})

		for _, tool := range tools {
			description := strings.TrimSpace(tool.Description)
			if description == "" {
				description = fmt.Sprintf("MCP tool %s/%s", serverName, tool.Name)
			}

			annotations := make([]string, 0, 2)
			if tool.ReadOnly {
				annotations = append(annotations, "readOnly")
			}
			if tool.Destructive {
				annotations = append(annotations, "destructive")
			}
			if len(annotations) > 0 {
				description += " (" + strings.Join(annotations, ", ") + ")"
			}

			schemas = append(schemas, v2.ToolSchema{
				Name:        "mcp__" + safeServer + "__" + NormalizeName(tool.Name),
				Description: description,
				Parameters:  cloneSchema(tool.InputSchema),
			})
		}
	}

	return schemas
}

// IsMCPTool 对标 Python handlers[prefixed] 的 MCP 命名判断。
//
// 判断一个 tool call 是否属于动态 MCP tool。
func (r *Registry) IsMCPTool(name string) bool {
	return strings.HasPrefix(name, "mcp__")
}

// Execute 对标 Python assemble_tool_pool 中 MCP handler lambda。
//
// 根据 mcp__{server}__{tool} 找到原始 server/tool，并调用 MCPClient.call_tool。
func (r *Registry) Execute(
	_ context.Context,
	call v2.ToolCall,
) (string, error) {
	if r == nil {
		return "", fmt.Errorf("mcp registry is nil")
	}

	serverSafe, toolSafe, ok := ParseToolName(call.Name)
	if !ok {
		return "", fmt.Errorf("invalid MCP tool name %q", call.Name)
	}

	args := map[string]any{}
	if len(call.Arguments) > 0 {
		if err := json.Unmarshal(call.Arguments, &args); err != nil {
			return "", err
		}
	}

	var (
		selectedClient *Client
		originalTool   string
	)

	r.mu.Lock()
	serverNames := make([]string, 0, len(r.clients))
	for name := range r.clients {
		serverNames = append(serverNames, name)
	}
	sort.Strings(serverNames)

	for _, serverName := range serverNames {
		client := r.clients[serverName]
		if NormalizeName(serverName) != serverSafe {
			continue
		}

		for _, tool := range client.tools {
			if NormalizeName(tool.Name) == toolSafe {
				selectedClient = client
				originalTool = tool.Name
				break
			}
		}
	}
	r.mu.Unlock()

	if selectedClient == nil {
		return fmt.Sprintf("MCP error: unknown tool %q", call.Name), nil
	}

	return selectedClient.CallTool(originalTool, args), nil
}

var disallowedNameChars = regexp.MustCompile(`[^a-zA-Z0-9_-]`)

// NormalizeName 对标 Python normalize_mcp_name。
//
// 将非 [a-zA-Z0-9_-] 字符替换为下划线，确保 MCP 工具名能进入 OpenAI tool schema。
func NormalizeName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "_"
	}

	return disallowedNameChars.ReplaceAllString(name, "_")
}

// ParseToolName 对标 Python mcp__{server}__{tool} 命名约定。
//
// 解析动态 MCP tool name。
func ParseToolName(name string) (server string, tool string, ok bool) {
	parts := strings.SplitN(name, "__", 3)
	if len(parts) != 3 || parts[0] != "mcp" {
		return "", "", false
	}
	if parts[1] == "" || parts[2] == "" {
		return "", "", false
	}

	return parts[1], parts[2], true
}

// mockServerDocs 对标 Python _mock_server_docs。
//
// 提供教学版 docs MCP server：search 和 get_version 都是 readOnly 工具。
func mockServerDocs() *Client {
	client := NewClient("docs")

	client.Register(
		[]ToolDefinition{
			{
				Name:        "search",
				Description: "Search documentation.",
				InputSchema: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"query": map[string]any{
							"type": "string",
						},
					},
					"required":             []string{"query"},
					"additionalProperties": false,
				},
				ReadOnly: true,
			},
			{
				Name:        "get_version",
				Description: "Get API version.",
				InputSchema: map[string]any{
					"type":                 "object",
					"properties":           map[string]any{},
					"required":             []string{},
					"additionalProperties": false,
				},
				ReadOnly: true,
			},
		},
		map[string]Handler{
			"search": func(args map[string]any) (string, error) {
				query, err := stringArg(args, "query")
				if err != nil {
					return "", err
				}

				return fmt.Sprintf("[docs] Found 3 results for %q", query), nil
			},
			"get_version": func(_ map[string]any) (string, error) {
				return "[docs] API v2.1.0", nil
			},
		},
	)

	return client
}

// mockServerDeploy 对标 Python _mock_server_deploy。
//
// 提供教学版 deploy MCP server：trigger 标记 destructive，status 标记 readOnly。
func mockServerDeploy() *Client {
	client := NewClient("deploy")

	client.Register(
		[]ToolDefinition{
			{
				Name:        "trigger",
				Description: "Trigger a deployment.",
				InputSchema: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"service": map[string]any{
							"type": "string",
						},
					},
					"required":             []string{"service"},
					"additionalProperties": false,
				},
				Destructive: true,
			},
			{
				Name:        "status",
				Description: "Check deployment status.",
				InputSchema: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"service": map[string]any{
							"type": "string",
						},
					},
					"required":             []string{"service"},
					"additionalProperties": false,
				},
				ReadOnly: true,
			},
		},
		map[string]Handler{
			"trigger": func(args map[string]any) (string, error) {
				service, err := stringArg(args, "service")
				if err != nil {
					return "", err
				}

				return "[deploy] Triggered: " + service, nil
			},
			"status": func(args map[string]any) (string, error) {
				service, err := stringArg(args, "service")
				if err != nil {
					return "", err
				}

				return fmt.Sprintf("[deploy] %s: running (v1.4.2)", service), nil
			},
		},
	)

	return client
}

// stringArg 对标 Python handler(**args) 的必填字符串参数检查。
//
// 教学版 mock handler 只需要少量字符串参数，这里集中做空值校验。
func stringArg(args map[string]any, key string) (string, error) {
	raw, ok := args[key]
	if !ok {
		return "", fmt.Errorf("%s is required", key)
	}

	text := strings.TrimSpace(fmt.Sprint(raw))
	if text == "" {
		return "", fmt.Errorf("%s is required", key)
	}

	return text, nil
}

// cloneSchema 对标 Python dict(tool_def["inputSchema"])。
//
// 转换为 ToolSchema 前复制一份 inputSchema，避免动态工具池组装时意外改动 MCP 原始定义。
func cloneSchema(schema map[string]any) map[string]any {
	if schema == nil {
		return map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		}
	}

	raw, err := json.Marshal(schema)
	if err != nil {
		return schema
	}

	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return schema
	}

	return out
}
