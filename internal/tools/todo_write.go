package tools

import (
	v2 "AgentLoop/internal/toolkit/v2"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
)

type TodoWriteStatus string

const (
	TodoWritePending    TodoWriteStatus = "pending"
	TodoWriteInProgress TodoWriteStatus = "in_progress"
	TodoWriteCompleted  TodoWriteStatus = "completed"
)

type TodoWriteItem struct {
	Content string          `json:"content"`
	Status  TodoWriteStatus `json:"status"`
}

type TodoWriteArgs struct {
	Todos []TodoWriteItem `json:"todos"`
}

// TodoWriteList 对标 Python todo_write session state。
//
// 它只维护当前 coding session 的任务列表。
type TodoWriteList struct {
	mu    sync.Mutex
	todos []TodoWriteItem
}

// NewTodoWriteList 对标 Python todo_write 全局状态初始化。
//
// 创建当前会话使用的内存任务列表。
func NewTodoWriteList() *TodoWriteList {
	return &TodoWriteList{
		todos: make([]TodoWriteItem, 0),
	}
}

func (l *TodoWriteList) Update(todos []TodoWriteItem) (string, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	for i, t := range todos {
		t.Content = strings.TrimSpace(t.Content)
		t.Status = TodoWriteStatus(strings.ToLower(strings.TrimSpace(string(t.Status))))

		if t.Content == "" {
			return "", fmt.Errorf("todos[%d] missing 'content'", i)
		}

		switch t.Status {
		case TodoWritePending, TodoWriteInProgress, TodoWriteCompleted:
		default:
			return "", fmt.Errorf("todos[%d] has invalid status %q", i, t.Status)
		}

		todos[i] = t
	}

	l.todos = todos

	// 对齐 Python：工具内部打印当前任务列表。
	fmt.Println(l.renderLocked())

	return fmt.Sprintf("Updated %d tasks", len(l.todos)), nil
}

func (l *TodoWriteList) Render() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.renderLocked()
}

func (l *TodoWriteList) renderLocked() string {
	if len(l.todos) == 0 {
		return "## Current Tasks\n  (empty)"
	}

	var b strings.Builder
	b.WriteString("\n\033[33m## Current Tasks\033[0m\n")

	for _, t := range l.todos {
		icon := " "

		switch t.Status {
		case TodoWritePending:
			icon = " "
		case TodoWriteInProgress:
			icon = "\033[36m▸\033[0m"
		case TodoWriteCompleted:
			icon = "\033[32m✓\033[0m"
		}

		fmt.Fprintf(&b, "  [%s] %s\n", icon, t.Content)
	}

	return strings.TrimRight(b.String(), "\n")
}
func executeTodoWrite(list *TodoWriteList) func(context.Context, json.RawMessage) (string, error) {
	return func(_ context.Context, arguments json.RawMessage) (string, error) {
		var args TodoWriteArgs
		if err := json.Unmarshal(arguments, &args); err != nil {
			return "", err
		}

		if args.Todos == nil {
			return "", fmt.Errorf("todos is required")
		}

		return list.Update(args.Todos)
	}
}

var DefaultTodoWriteList = NewTodoWriteList()

func NewTodoWriteToolV2() v2.Tool {
	return NewTodoWriteToolV2WithList(DefaultTodoWriteList)
}
func NewTodoWriteToolV2WithList(list *TodoWriteList) v2.Tool {
	if list == nil {
		list = NewTodoWriteList()
	}

	return v2.NewFunctionTool(
		"todo_write",
		"Create and manage a task list for your current coding session.",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"todos": map[string]any{
					"type":        "array",
					"description": "Task list for the current coding session.",
					"items": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"content": map[string]any{
								"type":        "string",
								"description": "Task description.",
							},
							"status": map[string]any{
								"type":        "string",
								"description": "Task status.",
								"enum": []string{
									"pending",
									"in_progress",
									"completed",
								},
							},
						},
						"required":             []string{"content", "status"},
						"additionalProperties": false,
					},
				},
			},
			"required":             []string{"todos"},
			"additionalProperties": false,
		},
		executeTodoWrite(list),
	)
}
