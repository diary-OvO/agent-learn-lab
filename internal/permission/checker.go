package permission

import (
	v2 "AgentLoop/internal/toolkit/v2"
	"AgentLoop/internal/tools"
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// permission.go 实现 S03 的工具执行权限检查。
// 当前采用三道门：
// 1. Gate 1：硬拒绝列表，命中后直接拒绝。
// 2. Gate 2：规则匹配，识别需要人工确认的高风险操作。
// 3. Gate 3：用户确认，由命令行读取用户是否允许继续执行。

// denyList 定义永远不允许执行的高危命令片段。
// 命中该列表时会直接拒绝，不进入用户确认。
var denyList = []string{
	"rm -rf /",
	"sudo",
	"shutdown",
	"reboot",
	"mkfs",
	"dd if=",
	"> /dev/sda",
}

// PermissionChecker 负责在工具执行前做权限检查。
// reader 用于和主流程复用同一个输入缓冲，避免 bufio.Reader 混用导致用户确认读取异常。
type PermissionChecker struct {
	reader *bufio.Reader
}

// NewPermissionChecker 使用 os.Stdin 创建默认权限检查器。
func NewPermissionChecker() *PermissionChecker {
	return NewPermissionCheckerWithReader(bufio.NewReader(os.Stdin))
}

// NewPermissionCheckerWithReader 使用指定 reader 创建权限检查器。
// 传入 nil 时会回退到 os.Stdin。
func NewPermissionCheckerWithReader(reader *bufio.Reader) *PermissionChecker {
	if reader == nil {
		reader = bufio.NewReader(os.Stdin)
	}
	return &PermissionChecker{
		reader: reader,
	}
}

// CheckPermission 执行完整的三道门权限检查。
// 返回 true 表示允许执行工具，返回 false 表示阻止本次工具调用。
func (p *PermissionChecker) CheckPermission(_ context.Context, call v2.ToolCall) bool {
	args, err := decodeToolArgs(call.Arguments)
	if err != nil {
		fmt.Printf("\n\033[31m⛔ 参数解析失败: %v\033[0m\n", err)
		return false
	}

	// Gate 1：硬拒绝列表。当前只对 bash 命令做命令片段匹配。
	if call.Name == "bash" {
		command := stringArg(args, "command")

		if reason := checkDenyList(command); reason != "" {
			fmt.Printf("\033[31m  permission gate 1: denied - %s\033[0m\n", reason)
			return false
		}

		fmt.Printf("\033[90m  permission gate 1: deny-list pass\033[0m\n")
	} else {
		fmt.Printf("\033[90m  permission gate 1: deny-list skipped\033[0m\n")
	}

	// Gate 2：规则匹配。命中后进入 Gate 3 用户确认。
	if reason := checkPermissionRules(call.Name, args); reason != "" {
		fmt.Printf("\033[33m  permission gate 2: approval required - %s\033[0m\n", reason)
		return p.askUser(call.Name, args, reason)
	}

	fmt.Printf("\033[90m  permission gate 2: rules pass\033[0m\n")
	fmt.Printf("\033[32m  permission: allowed\033[0m\n")
	return true
}

// checkDenyList 检查 bash 命令是否命中硬拒绝列表。
// 返回非空字符串表示拒绝原因；返回空字符串表示未命中。
func checkDenyList(command string) string {
	for _, pattern := range denyList {
		if strings.Contains(command, pattern) {
			return fmt.Sprintf("Blocked: %q is on the deny list", pattern)
		}
	}
	return ""
}

// checkPermissionRules 检查工具调用是否命中需要人工确认的规则。
// 返回非空字符串表示需要用户审批的原因；返回空字符串表示规则放行。
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
			"del ",
			"erase ",
			"rmdir ",
			"rd ",
			"remove-item",
		}

		command = strings.ToLower(command)
		for _, kw := range riskyKeywords {
			if strings.Contains(command, kw) {
				return "Potentially destructive command"
			}
		}
	}

	return ""
}

// askUser 执行 Gate 3 用户确认。
// 只有输入 y 或 yes 时才允许继续执行，其它输入默认拒绝。
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
	allowed := choice == "y" || choice == "yes"
	if allowed {
		fmt.Printf("\033[32m  permission gate 3: user allowed\033[0m\n")
	} else {
		fmt.Printf("\033[31m  permission gate 3: user denied\033[0m\n")
	}
	return allowed
}

// decodeToolArgs 将模型返回的工具参数 JSON 解码为 map。
// 空参数会被视为一个空 map，方便后续统一读取字段。
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

// stringArg 从工具参数中读取字符串字段，并做 trim。
// 字段不存在或值为 nil 时返回空字符串。
func stringArg(args map[string]any, key string) string {
	v, ok := args[key]
	if !ok || v == nil {
		return ""
	}

	return strings.TrimSpace(fmt.Sprint(v))
}

// compactArgs 压缩工具参数，避免权限确认时把大文件内容完整刷屏。
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

// previewPermissionArg 截断单个字符串参数，按 rune 处理避免截断中文字符。
func previewPermissionArg(s string, limit int) string {
	runes := []rune(s)

	if len(runes) <= limit {
		return s
	}

	return string(runes[:limit]) + "...<truncated>"
}
