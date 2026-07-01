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
	board tasks.Board,
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

		return board.Claim(args.TaskID, "agent")
	}
}

// executeClaimTaskWithOwner 对标 Python S17 teammate _run_claim_task。
//
// 使用指定 owner 认领任务，并启用 S17 owner 检查；旧 executeClaimTask 保持 S12-S16 语义。
// 迭代原因：S17 teammate 需要用自己的 agent name 写入 owner，否则任务板无法区分是谁自主认领了任务。
// 与旧函数差别：executeClaimTask 固定 owner=agent 且调用 Board.Claim；executeClaimTaskWithOwner 绑定调用方传入的 owner，并调用 Board.ClaimWithOwnerCheck。
func executeClaimTaskWithOwner(
	board tasks.Board,
	owner string,
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

		return board.ClaimWithOwnerCheck(args.TaskID, owner)
	}
}

// NewClaimTaskToolV2 对标 Python claim_task tool schema。
//
// 注册认领 pending 任务的工具。
func NewClaimTaskToolV2(board tasks.Board) v2.Tool {
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
		executeClaimTask(board),
	)
}

// NewClaimTaskToolV2WithOwner 对标 Python S17 teammate claim_task。
//
// 注册带固定 owner 的 claim_task；S17 teammate 用自己的 agent name 认领任务。
// 迭代原因：工具 schema 仍然叫 claim_task，但 S17 teammate 的执行语义要带 owner 检查，不能影响 Lead 和旧课程的 claim_task。
// 与旧函数差别：NewClaimTaskToolV2 注册的是旧版 Lead claim；NewClaimTaskToolV2WithOwner 注册同名工具的新执行闭包，只在 S17 teammate/Lead 自主任务路径中显式选择。
func NewClaimTaskToolV2WithOwner(
	board tasks.Board,
	owner string,
) v2.Tool {
	return v2.NewFunctionTool(
		"claim_task",
		"Claim a pending task for this teammate. Fails if the task is already owned.",
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
		executeClaimTaskWithOwner(board, owner),
	)
}
