package tools

import (
	v2 "AgentLoop/00-mini_agent_loop/openai_model/tools/v2"
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
)

const maxTodos = 20

type TodoStatus string

const (
	TodoPending    TodoStatus = "pending"
	TodoInProgress TodoStatus = "in_progress"
	TodoCompleted  TodoStatus = "completed"
)

type TodoItem struct {
	ID     string     `json:"id"`
	Text   string     `json:"text"`
	Status TodoStatus `json:"status"`
}

type TodoArgs struct {
	Items []TodoItem `json:"items"`
}

// TodoManager 在AgentLoop中新建出来的状态管理器，通过function call的形式进行管理
type TodoManager struct {
	mu    sync.Mutex
	items []TodoItem
}

func NewTodoManager() *TodoManager {
	return &TodoManager{
		items: make([]TodoItem, 0),
	}
}

func (m *TodoManager) Update(items []TodoItem) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if len(items) > maxTodos {
		return "", fmt.Errorf("max %d todos allowed", maxTodos)
	}

	validated := make([]TodoItem, 0, len(items))

	inProgressCount := 0

	for i, item := range items {
		item.ID = strings.TrimSpace(item.ID)
		item.Text = strings.TrimSpace(item.Text)
		item.Status = TodoStatus(strings.ToLower(strings.TrimSpace(string(item.Status))))

		if item.ID == "" {
			item.ID = strconv.Itoa(i + 1)
		}

		if item.Status == "" {
			item.Status = TodoPending
		}

		if item.Text == "" {
			return "", fmt.Errorf("item %s: text required", item.ID)
		}

		switch item.Status {
		case TodoPending:
		case TodoInProgress:
			inProgressCount++
		case TodoCompleted:
		default:
			return "", fmt.Errorf("item %s: invalid status %q", item.ID, item.Status)
		}

		validated = append(validated, item)
	}
	if inProgressCount > 1 {
		return "", fmt.Errorf("only one task can be in_progress at a time")
	}

	m.items = validated
	return m.renderLocked(), nil
}

// Render 返回当前 todo 状态。
// 对外读取时加锁，避免和 Update 并发冲突。
func (m *TodoManager) Render() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.renderLocked()
}

// renderLocked 要求调用方已经持有锁。
// 这样 Update 内部可以避免重复加锁。
func (m *TodoManager) renderLocked() string {
	if len(m.items) == 0 {
		return "No todos."
	}
	var b strings.Builder
	done := 0
	for _, item := range m.items {
		marker := "[ ]"

		switch item.Status {
		case TodoPending:
			marker = "[ ]"
		case TodoInProgress:
			marker = "[>]"
		case TodoCompleted:
			marker = "[x]"
			done++
		}
		fmt.Fprintf(&b, "%s #%s: %s\n", marker, item.ID, item.Text)
	}
	fmt.Fprintf(&b, "\n(%d/%d completed)", done, len(m.items))
	return strings.TrimRight(b.String(), "\n")
}

func executeTodoWrite(manager *TodoManager) func(context.Context, json.RawMessage) (string, error) {
	return func(_ context.Context, arguments json.RawMessage) (string, error) {
		var args TodoArgs
		if err := json.Unmarshal(arguments, &args); err != nil {
			return "", err
		}

		if args.Items == nil {
			return "", fmt.Errorf("items is required")
		}

		return manager.Update(args.Items)
	}
}

var DefaultTodoManager = NewTodoManager()

func NewTodoToolV2WithManager(manager *TodoManager) v2.Tool {
	if manager == nil {
		manager = NewTodoManager()
	}

	return v2.NewFunctionTool(
		"todo",
		"Update task list. Track progress on multi-step tasks.",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"items": map[string]any{
					"type":        "array",
					"description": "Full todo list. Use it to plan and update progress.",
					"items": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"id": map[string]any{
								"type":        "string",
								"description": "Stable todo id, such as 1, 2, 3.",
							},
							"text": map[string]any{
								"type":        "string",
								"description": "Concrete task description.",
							},
							"status": map[string]any{
								"type":        "string",
								"description": "Todo status.",
								"enum":        []string{"pending", "in_progress", "completed"},
							},
						},
						"required":             []string{"id", "text", "status"},
						"additionalProperties": false,
					},
				},
			},
			"required":             []string{"items"},
			"additionalProperties": false,
		},
		executeTodoWrite(manager),
	)
}

func NewTodoToolV2() v2.Tool {
	return NewTodoToolV2WithManager(DefaultTodoManager)
}
