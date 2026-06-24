package tools

import (
	"AgentLoop/internal/tasks"
	v2 "AgentLoop/internal/toolkit/v2"
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// executeCompleteTask 对标 Python run_complete_task。
//
// 完成一个 in_progress 任务，并返回被解除依赖的下游任务。
func executeCompleteTask(
	store tasks.Store,
) func(context.Context, json.RawMessage) (string, error) {
	return func(_ context.Context, arguments json.RawMessage) (string, error) {
		var args TaskIDArgs
		if err := json.Unmarshal(arguments, &args); err != nil {
			return "", err
		}

		args.TaskID = strings.TrimSpace(args.TaskID)
		if args.TaskID == "" {
			return "", fmt.Errorf("task_id is required")
		}

		return store.Complete(args.TaskID)
	}
}

// NewCompleteTaskToolV2 对标 Python complete_task tool schema。
//
// 注册完成 in_progress 任务并报告解除依赖任务的工具。
func NewCompleteTaskToolV2(store tasks.Store) v2.Tool {
	return v2.NewFunctionTool(
		"complete_task",
		"Complete an in-progress task. Reports unblocked downstream tasks.",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"task_id": map[string]any{
					"type":        "string",
					"description": "ID of the task to complete.",
				},
			},
			"required":             []string{"task_id"},
			"additionalProperties": false,
		},
		executeCompleteTask(store),
	)
}
