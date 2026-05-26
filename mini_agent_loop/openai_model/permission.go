package openai_model

import (
	"AgentLoop/mini_agent_loop/openai_model/tools"
	v2 "AgentLoop/mini_agent_loop/openai_model/tools/v2"
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

//文件作用，三步确认原则

//首先定义硬拦截的名单

var denyList = []string{
	"rm -rf /",
	"sudo",
	"shutdown",
	"reboot",
	"mkfs",
	"dd if=",
	"> /dev/sda",
}

// 提供一个对象，用于与主流程公用一个buffer
type PermissionChecker struct {
	reader *bufio.Reader
}

func NewPermissionChecker() *PermissionChecker {
	return NewPermissionCheckerWithReader(bufio.NewReader(os.Stdin))
}
func NewPermissionCheckerWithReader(reader *bufio.Reader) *PermissionChecker {
	if reader == nil {
		reader = bufio.NewReader(os.Stdin)
	}
	return &PermissionChecker{
		reader: reader,
	}
}

func (p *PermissionChecker) CheckPermission(_ context.Context, call v2.ToolCall) bool {
	args, err := decodeToolArgs(call.Arguments)
	if err != nil {
		fmt.Printf("\n\033[31m⛔ 参数解析失败: %v\033[0m\n", err)
		return false
	}

	//Gate 1：硬拒绝的列表
	if call.Name == "bash" {
		command := stringArg(args, "command")

		if reason := checkDenyList(command); reason != "" {
			fmt.Printf("\n\033[31m⛔ %s\033[0m\n", reason)
			return false
		}
	}

	// Gate 2: 规则匹配
	// 需要人工确认的命令
	if reason := checkPermissionRules(call.Name, args); reason != "" {
		return p.askUser(call.Name, args, reason)
	}

	return true
}

// Gate 1：硬拒绝。
// 命中后直接拒绝，不询问用户。
func checkDenyList(command string) string {
	for _, pattern := range denyList {
		if strings.Contains(command, pattern) {
			return fmt.Sprintf("Blocked: %q is on the deny list", pattern)
		}
	}
	return ""
}

// Gate 2：规则匹配。
// 命中规则后进入用户确认。
func checkPermissionRules(toolName string, args map[string]any) string {
	switch toolName {
	case "write_file", "edit_file":
		path := stringArg(args, "path")
		if path == "" {
			return ""
		}
		// safePath 来自你前面实现的文件安全函数。
		// 如果路径逃出工作区，就要求人工确认。
		if _, err := tools.SafePath(path); err != nil {
			return "Writing outside workspace"
		}
	case "bash":
		command := stringArg(args, "command")
		if command == "" {
			return ""
		}
		// 这些命令不一定永远禁止，但属于高风险操作。
		// 所以不直接拦截，而是让用户审批。
		riskyKeywords := []string{
			"rm ",
			"> /etc/",
			"chmod 777",
		}

		for _, kw := range riskyKeywords {
			if strings.Contains(command, kw) {
				return "Potentially destructive command"
			}
		}
	}

	return ""
}

// Gate 3：用户确认
func (p *PermissionChecker) askUser(toolName string, args map[string]any, reason string) bool {
	fmt.Printf("\n\033[33m⚠️  %s\033[0m\n", reason)
	fmt.Printf("   Tool: %s(%v)\n", toolName, compactArgs(args))
	fmt.Print("   Allow? [y/N] ")

	line, err := p.reader.ReadString('\n')
	if err != nil && strings.TrimSpace(line) == "" {
		fmt.Printf("\n读取用户确认失败: %v\n", err)
		return false
	}

	choice := strings.ToLower(strings.TrimSpace(line))
	return choice == "y" || choice == "yes"
}

func decodeToolArgs(raw json.RawMessage) (map[string]any, error) {
	if len(raw) == 0 {
		return map[string]any{}, nil
	}

	var args map[string]any
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, err
	}

	return args, nil
}
func stringArg(args map[string]any, key string) string {
	v, ok := args[key]
	if !ok || v == nil {
		return ""
	}

	return strings.TrimSpace(fmt.Sprint(v))
}

// compactArgs 用于打印参数时避免把大文件内容完整刷屏。
func compactArgs(args map[string]any) map[string]any {
	out := make(map[string]any, len(args))

	for k, v := range args {
		if s, ok := v.(string); ok {
			out[k] = previewPermissionArg(s, 160)
		} else {
			out[k] = v
		}
	}

	return out
}

func previewPermissionArg(s string, limit int) string {
	runes := []rune(s)

	if len(runes) <= limit {
		return s
	}

	return string(runes[:limit]) + "...<truncated>"
}
