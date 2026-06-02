package agentconsole

import (
	v2 "AgentLoop/internal/toolkit/v2"
	"context"
	"fmt"
	"strings"
	"time"
)

// PrintToolCall prints a formatted tool call for lessons that do not use hooks.
func PrintToolCall(ctx context.Context, call v2.ToolCall) {
	line := FormatToolCall(ctx, call)
	if strings.TrimSpace(line) != "" {
		fmt.Println(line)
	}
}

// PrintToolResult prints a formatted tool result for lessons that do not use hooks.
func PrintToolResult(ctx context.Context, call v2.ToolCall, result string) {
	line := FormatToolResult(ctx, call, result)
	if strings.TrimSpace(line) != "" {
		fmt.Println(line)
	}
}

// PrintSubagentStart prints a formatted subagent start event for direct output.
func PrintSubagentStart(ctx context.Context, id string, description string) {
	line := FormatSubagentStart(ctx, id, description)
	if strings.TrimSpace(line) != "" {
		fmt.Println(line)
	}
}

// PrintSubagentDone prints a formatted subagent completion event for direct output.
func PrintSubagentDone(ctx context.Context, id string, summary string, elapsed time.Duration) {
	line := FormatSubagentDone(ctx, id, summary, elapsed)
	if strings.TrimSpace(line) != "" {
		fmt.Println(line)
	}
}

// PrintSubagentError prints a formatted subagent error event for direct output.
func PrintSubagentError(ctx context.Context, id string, err error, elapsed time.Duration) {
	line := FormatSubagentError(ctx, id, err, elapsed)
	if strings.TrimSpace(line) != "" {
		fmt.Println(line)
	}
}
