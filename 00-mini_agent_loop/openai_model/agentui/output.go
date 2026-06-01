package agentui

import (
	v2 "AgentLoop/00-mini_agent_loop/openai_model/tools/v2"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type AgentScope struct {
	Name  string
	ID    string
	Depth int
}

type scopeKey struct{}

func WithAgentScope(ctx context.Context, scope AgentScope) context.Context {
	return context.WithValue(ctx, scopeKey{}, scope)
}

func ScopeFromContext(ctx context.Context) AgentScope {
	scope, ok := ctx.Value(scopeKey{}).(AgentScope)
	if !ok {
		return AgentScope{Name: "main", ID: "parent", Depth: 0}
	}

	if scope.Name == "" {
		scope.Name = "main"
	}

	if scope.ID == "" {
		scope.ID = "parent"
	}

	return scope
}

func PrintToolCall(ctx context.Context, call v2.ToolCall) {
	line := FormatToolCall(ctx, call)
	if strings.TrimSpace(line) != "" {
		fmt.Println(line)
	}
}

func PrintToolResult(ctx context.Context, call v2.ToolCall, result string) {
	line := FormatToolResult(ctx, call, result)
	if strings.TrimSpace(line) != "" {
		fmt.Println(line)
	}
}

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

	default:
		return fmt.Sprintf(
			"%s args=%s",
			prefix,
			compactJSON(call.Arguments, 220),
		)
	}
}

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

	default:
		return fmt.Sprintf("%s %s", prefix, Preview(result, 300))
	}
}

func PrintSubagentStart(ctx context.Context, id string, description string) {
	scope := ScopeFromContext(ctx)

	fmt.Printf(
		"\n\033[35m[Subagent spawned] id=%s parent=%s depth=%d\033[0m\n  task: %s\n",
		id,
		formatScope(scope),
		scope.Depth,
		Preview(description, 260),
	)
}

func PrintSubagentDone(ctx context.Context, id string, summary string, elapsed time.Duration) {
	fmt.Printf(
		"\033[35m[Subagent done] id=%s elapsed=%s\033[0m\n  summary: %s\n",
		id,
		elapsed.Round(time.Millisecond),
		Preview(strings.TrimSpace(summary), 360),
	)
}

func PrintSubagentError(ctx context.Context, id string, err error, elapsed time.Duration) {
	fmt.Printf(
		"\033[31m[Subagent error] id=%s elapsed=%s error=%v\033[0m\n",
		id,
		elapsed.Round(time.Millisecond),
		err,
	)
}

func Preview(s string, limit int) string {
	s = strings.TrimSpace(s)
	if limit <= 0 {
		return s
	}

	runes := []rune(s)
	if len(runes) <= limit {
		return s
	}

	return string(runes[:limit]) + "...<truncated>"
}

func formatScope(scope AgentScope) string {
	if scope.Name == "" {
		scope.Name = "main"
	}

	if scope.ID == "" || scope.ID == "parent" {
		return scope.Name
	}

	return scope.Name + ":" + scope.ID
}

func decodeArgs(raw json.RawMessage) map[string]any {
	if len(raw) == 0 {
		return map[string]any{}
	}

	var args map[string]any
	if err := json.Unmarshal(raw, &args); err != nil {
		return map[string]any{
			"_raw": string(raw),
		}
	}

	return args
}

func argString(args map[string]any, key string) string {
	v, ok := args[key]
	if !ok || v == nil {
		return ""
	}

	switch x := v.(type) {
	case string:
		return x
	default:
		return fmt.Sprint(x)
	}
}

func argValue(args map[string]any, key string) string {
	v, ok := args[key]
	if !ok || v == nil {
		return "-"
	}

	return fmt.Sprint(v)
}

func quote(s string) string {
	if s == "" {
		return `""`
	}

	return fmt.Sprintf("%q", s)
}

func compactJSON(raw json.RawMessage, limit int) string {
	if len(raw) == 0 {
		return "{}"
	}

	var decoded any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return Preview(string(raw), limit)
	}

	b, err := json.Marshal(decoded)
	if err != nil {
		return Preview(string(raw), limit)
	}

	return Preview(string(b), limit)
}

func formatTodoWriteArgs(args map[string]any) string {
	rawTodos, ok := args["todos"].([]any)
	if !ok {
		return "todos=?"
	}

	pending := 0
	inProgress := 0
	completed := 0

	for _, item := range rawTodos {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}

		status := strings.ToLower(strings.TrimSpace(fmt.Sprint(m["status"])))

		switch status {
		case "pending":
			pending++
		case "in_progress":
			inProgress++
		case "completed":
			completed++
		}
	}

	return fmt.Sprintf(
		"todos=%d pending=%d in_progress=%d completed=%d",
		len(rawTodos),
		pending,
		inProgress,
		completed,
	)
}

func indent(s string, prefix string) string {
	if strings.TrimSpace(s) == "" {
		return ""
	}

	lines := strings.Split(s, "\n")
	for i := range lines {
		lines[i] = prefix + lines[i]
	}

	return strings.Join(lines, "\n")
}
