package tools

import (
	"AgentLoop/internal/tasks"
	v2 "AgentLoop/internal/toolkit/v2"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// executeGetTask 对标 Python run_get_task。
//
// 根据 task_id 返回完整任务 JSON；任务不存在时返回可读错误。
func executeGetTask(
	store tasks.Store,
) func(context.Context, json.RawMessage) (string, error) {
	return func(
		_ context.Context,
		arguments json.RawMessage,
	) (string, error) {
		var args TaskIDArgs
		if err := json.Unmarshal(arguments, &args); err != nil {
			return "", err
		}

		args.TaskID = strings.TrimSpace(args.TaskID)
		if args.TaskID == "" {
			return "", fmt.Errorf("task_id is required")
		}

		result, err := store.Get(args.TaskID)
		if os.IsNotExist(err) {
			return fmt.Sprintf("Error: Task %s not found", args.TaskID), nil
		}
		if err != nil {
			return "", err
		}

		return result, nil
	}
}

// NewGetTaskToolV2 对标 Python get_task tool schema。
//
// 注册读取单个任务完整信息的工具。
func NewGetTaskToolV2(store tasks.Store) v2.Tool {
	return v2.NewFunctionTool(
		"get_task",
		"Get full details of a specific task by ID.",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"task_id": map[string]any{
					"type":        "string",
					"description": "ID of the task to inspect.",
				},
			},
			"required":             []string{"task_id"},
			"additionalProperties": false,
		},
		executeGetTask(store),
	)
}
