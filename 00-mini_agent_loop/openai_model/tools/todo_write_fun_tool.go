package tools

import (
	v2 "AgentLoop/00-mini_agent_loop/openai_model/tools/v2"
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

// TodoWriteManager
// 它只维护当前 coding session 的任务列表。
type TodoWriteManager struct {
	mu    sync.Mutex
	todos []TodoWriteItem
}

func NewTodoWriteManager() *TodoWriteManager {
	return &TodoWriteManager{
		todos: make([]TodoWriteItem, 0),
	}
}

func (m *TodoWriteManager) Update(todos []TodoWriteItem) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

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

	m.todos = todos

	// 对齐 Python：工具内部打印当前任务列表。
	fmt.Println(m.renderLocked())

	return fmt.Sprintf("Updated %d tasks", len(m.todos)), nil
}

func (m *TodoWriteManager) Render() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.renderLocked()
}

func (m *TodoWriteManager) renderLocked() string {
	if len(m.todos) == 0 {
		return "## Current Tasks\n  (empty)"
	}

	var b strings.Builder
	b.WriteString("\n\033[33m## Current Tasks\033[0m\n")

	for _, t := range m.todos {
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
func executeTodoWrite(manager *TodoWriteManager) func(context.Context, json.RawMessage) (string, error) {
	return func(_ context.Context, arguments json.RawMessage) (string, error) {
		var args TodoWriteArgs
		if err := json.Unmarshal(arguments, &args); err != nil {
			return "", err
		}

		if args.Todos == nil {
			return "", fmt.Errorf("todos is required")
		}

		return manager.Update(args.Todos)
	}
}

var DefaultTodoWriteManager = NewTodoWriteManager()

func NewTodoWriteToolV2() v2.Tool {
	return NewTodoWriteToolV2WithManager(DefaultTodoWriteManager)
}
func NewTodoWriteToolV2WithManager(manager *TodoWriteManager) v2.Tool {
	if manager == nil {
		manager = NewTodoWriteManager()
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
		executeTodoWrite(manager),
	)
}
