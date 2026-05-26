package agentui

import (
	"encoding/json"
	"fmt"
	"strings"

	v2 "AgentLoop/00-mini_agent_loop/openai_model/tools/v2"
)

// PrintToolCall 打印工具名，并输出该工具对应的额外调用信息。
func PrintToolCall(call v2.ToolCall) {
	fmt.Printf("\033[36m> 喵喵正在使用 %s 工具\033[0m\n", call.Name)
	PrintBashCommand(call)
}

// PrintBashCommand 打印 bash 工具调用中的 command 参数。
func PrintBashCommand(call v2.ToolCall) {
	if call.Name != "bash" {
		return
	}

	var args struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal(call.Arguments, &args); err != nil || strings.TrimSpace(args.Command) == "" {
		fmt.Printf("\033[90m  command args: %s\033[0m\n", string(call.Arguments))
		return
	}

	command := strings.TrimSpace(args.Command)
	if strings.Contains(command, "\n") {
		fmt.Printf("\033[90m  command:\033[0m\n%s\n", command)
		return
	}

	fmt.Printf("\033[90m  command: %s\033[0m\n", command)
}

// FormatToolResult 格式化工具执行结果，供 CLI 输出展示。
func FormatToolResult(toolName string, result string) string {
	if toolName == "todo" {
		return FormatTodoProgress(result)
	}
	return Preview(result, 200)
}

// FormatTodoProgress 把 todo 工具结果格式化成可见的进度面板。
func FormatTodoProgress(result string) string {
	result = strings.TrimSpace(result)
	if result == "" {
		result = "No todos."
	}
	return "📌 当前 TodoList 进度\n" + result
}

// Preview 截断过长输出，并按 rune 处理避免截断中文字符。
func Preview(s string, limit int) string {
	runes := []rune(s)
	if len(runes) <= limit {
		return s
	}
	return string(runes[:limit]) + "\n...output truncated"
}
