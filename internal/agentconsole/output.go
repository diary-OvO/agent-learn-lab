// Package agentconsole owns console presentation for the learning agents.
//
// Format* functions are side-effect-free formatting APIs. They return strings
// for S04+ hooks to print from hookimpl.
//
// Print* functions are compatibility APIs for S01/S02/S03 and other lessons
// that have not been wired through hooks yet. Print* functions only call the
// corresponding Format* function, then write the formatted text to stdout.
//
// Once an Agent loop or SubAgent loop is wired through hooks, it should not
// call Print* directly. hookimpl should call Format* and decide how to output
// the returned text.
//
// This package only handles console display. It does not handle permissions,
// hook dispatch, tool execution, or model calls.
package agentconsole

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
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
