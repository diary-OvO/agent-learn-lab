package tools

import (
	"AgentLoop/internal/tasks"
	v2 "AgentLoop/internal/toolkit/v2"
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// CreateTaskArgs 对标 Python run_create_task 的参数。
//
// 表示创建持久化任务时需要的 subject、description 和 blockedBy。
type CreateTaskArgs struct {
	Subject     string   `json:"subject"`
	Description string   `json:"description"`
	BlockedBy   []string `json:"blockedBy"`
}

// executeCreateTask 对标 Python run_create_task。
//
// 解析工具参数、创建 pending 任务，并返回任务 ID 和依赖信息。
func executeCreateTask(
	store tasks.Store,
) func(context.Context, json.RawMessage) (string, error) {
	return func(
		_ context.Context,
		arguments json.RawMessage,
	) (string, error) {
		var args CreateTaskArgs
		if err := json.Unmarshal(arguments, &args); err != nil {
			return "", err
		}

		args.Subject = strings.TrimSpace(args.Subject)
		if args.Subject == "" {
			return "", fmt.Errorf("subject is required")
		}

		task, err := store.Create(args.Subject, args.Description, args.BlockedBy)
		if err != nil {
			return "", err
		}

		dependencies := ""
		if len(args.BlockedBy) > 0 {
			dependencies = fmt.Sprintf(" (blockedBy: %s)", strings.Join(args.BlockedBy, ", "))
		}

		// 对齐 Python：工具内部打印任务创建事件。
		fmt.Printf("  \033[34m[create] %s%s\033[0m\n", task.Subject, dependencies)

		return fmt.Sprintf("Created %s: %s%s", task.ID, task.Subject, dependencies), nil
	}
}

// NewCreateTaskToolV2 对标 Python create_task tool schema。
//
// 注册创建持久化任务的工具。
func NewCreateTaskToolV2(store tasks.Store) v2.Tool {
	return v2.NewFunctionTool(
		"create_task",
		"Create a new task with optional blockedBy dependencies.",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"subject": map[string]any{
					"type":        "string",
					"description": "Short task subject.",
				},
				"description": map[string]any{
					"type":        "string",
					"description": "Detailed task description.",
				},
				"blockedBy": map[string]any{
					"type":        "array",
					"description": "Task IDs that must be completed before this task can start.",
					"items": map[string]any{
						"type": "string",
					},
				},
			},
			"required":             []string{"subject"},
			"additionalProperties": false,
		},
		executeCreateTask(store),
	)
}
