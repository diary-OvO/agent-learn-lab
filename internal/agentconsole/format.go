package agentconsole

import (
	v2 "AgentLoop/internal/toolkit/v2"
	"context"
	"fmt"
	"strings"
	"time"
)

// FormatToolCall formats a tool call without printing it.
func FormatToolCall(ctx context.Context, call v2.ToolCall) string {
	scope := ScopeFromContext(ctx)
	args := decodeArgs(call.Arguments)

	prefix := fmt.Sprintf(
		"\033[36m> [%s] %s\033[0m",
		formatScope(scope),
		call.Name,
	)

	switch call.Name {
	case "bash":
		command := argString(args, "command")
		if command == "" {
			return prefix
		}
		return fmt.Sprintf("%s $ %s", prefix, Preview(command, 180))

	case "read_file":
		return fmt.Sprintf(
			"%s path=%s limit=%s",
			prefix,
			quote(argString(args, "path")),
			argValue(args, "limit"),
		)

	case "write_file":
		content := argString(args, "content")
		return fmt.Sprintf(
			"%s path=%s bytes=%d",
			prefix,
			quote(argString(args, "path")),
			len([]byte(content)),
		)

	case "edit_file":
		return fmt.Sprintf(
			"%s path=%s old=%s new=%s",
			prefix,
			quote(argString(args, "path")),
			quote(Preview(argString(args, "old_text"), 60)),
			quote(Preview(argString(args, "new_text"), 60)),
		)

	case "glob":
		return fmt.Sprintf(
			"%s pattern=%s",
			prefix,
			quote(argString(args, "pattern")),
		)

	case "todo_write":
		return fmt.Sprintf(
			"%s %s",
			prefix,
			formatTodoWriteArgs(args),
		)

	case "task":
		return fmt.Sprintf(
			"%s description=%s",
			prefix,
			quote(Preview(argString(args, "description"), 220)),
		)
	case "load_skill":
		return fmt.Sprintf(
			"%s name=%s",
			prefix,
			quote(argString(args, "name")),
		)
	default:
		return fmt.Sprintf(
			"%s args=%s",
			prefix,
			compactJSON(call.Arguments, 220),
		)
	}
}

// FormatToolResult formats a tool result without printing it.
func FormatToolResult(ctx context.Context, call v2.ToolCall, result string) string {
	scope := ScopeFromContext(ctx)

	result = strings.TrimSpace(result)
	if result == "" {
		return ""
	}

	prefix := fmt.Sprintf(
		"\033[90m< [%s] %s\033[0m",
		formatScope(scope),
		call.Name,
	)

	switch call.Name {
	case "task":
		return fmt.Sprintf("%s\n%s", prefix, indent(Preview(result, 800), "  "))

	case "bash":
		return fmt.Sprintf("%s\n%s", prefix, indent(Preview(result, 400), "  "))

	case "read_file":
		return fmt.Sprintf("%s\n%s", prefix, indent(Preview(result, 500), "  "))

	case "todo_write":
		return fmt.Sprintf("%s %s", prefix, Preview(result, 240))
	case "load_skill":
		return fmt.Sprintf("%s\n%s", prefix, indent(Preview(result, 800), "  "))
	default:
		return fmt.Sprintf("%s %s", prefix, Preview(result, 300))
	}
}

// FormatSubagentStart formats a subagent start event without printing it.
func FormatSubagentStart(ctx context.Context, id string, description string) string {
	scope := ScopeFromContext(ctx)

	return fmt.Sprintf(
		"\n\033[35m[Subagent spawned] id=%s parent=%s depth=%d\033[0m\n  task: %s",
		id,
		formatScope(scope),
		scope.Depth,
		Preview(description, 260),
	)
}

// FormatSubagentDone formats a subagent completion event without printing it.
func FormatSubagentDone(ctx context.Context, id string, summary string, elapsed time.Duration) string {
	return fmt.Sprintf(
		"\033[35m[Subagent done] id=%s elapsed=%s\033[0m\n  summary: %s",
		id,
		elapsed.Round(time.Millisecond),
		Preview(strings.TrimSpace(summary), 360),
	)
}

// FormatSubagentError formats a subagent error event without printing it.
func FormatSubagentError(ctx context.Context, id string, err error, elapsed time.Duration) string {
	return fmt.Sprintf(
		"\033[31m[Subagent error] id=%s elapsed=%s error=%v\033[0m",
		id,
		elapsed.Round(time.Millisecond),
		err,
	)
}
