package tools

import (
	"AgentLoop/internal/tasks"
	v2 "AgentLoop/internal/toolkit/v2"
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// executeClaimTask 对标 Python run_claim_task。
//
// 认领一个未阻塞的 pending 任务，并将 owner 设置为 agent。
func executeClaimTask(
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

		return store.Claim(args.TaskID, "agent")
	}
}

// NewClaimTaskToolV2 对标 Python claim_task tool schema。
//
// 注册认领 pending 任务的工具。
func NewClaimTaskToolV2(store tasks.Store) v2.Tool {
	return v2.NewFunctionTool(
		"claim_task",
		"Claim a pending task. Sets owner and changes status to in_progress.",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"task_id": map[string]any{
					"type":        "string",
					"description": "ID of the task to claim.",
				},
			},
			"required":             []string{"task_id"},
			"additionalProperties": false,
		},
		executeClaimTask(store),
	)
}
