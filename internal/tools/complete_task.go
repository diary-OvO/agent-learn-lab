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

		return board.Complete(args.TaskID)
	}
}

// executeCompleteTaskWithAfterComplete 对标 Python S18 teammate _run_complete_task。
//
// 迭代原因：S18 teammate 完成一个绑定 worktree 的任务后，后续空闲轮次不应继续沿用旧 cwd。
//
// 与 executeCompleteTask 差别：旧函数只调用 Board.Complete；S18 版本在完成成功后
// 触发 afterComplete 回调，让 Spawner 清空当前 worktree cwd。
func executeCompleteTaskWithAfterComplete(
	board tasks.Board,
	afterComplete func(tasks.Task),
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

		task, _ := board.Load(args.TaskID)

		result, err := board.Complete(args.TaskID)
		if err != nil {
			return "", err
		}

		if afterComplete != nil && strings.Contains(result, "Completed") {
			afterComplete(task)
		}

		return result, nil
	}
}

// NewCompleteTaskToolV2 对标 Python complete_task tool schema。
//
// 注册完成 in_progress 任务并报告解除依赖任务的工具。
func NewCompleteTaskToolV2(board tasks.Board) v2.Tool {
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
		executeCompleteTask(board),
	)
}

// NewCompleteTaskToolV2WithAfterComplete 对标 Python S18 complete_task teammate tool schema。
//
// 迭代原因：S18 teammate 的 complete_task 需要通知 Spawner 当前任务结束，以便退出
// worktree cwd；Lead 和旧课程不需要这个副作用。
//
// 与 NewCompleteTaskToolV2 差别：两者注册同名 complete_task schema；S18 版本额外注入
// afterComplete 回调，只在 S18 teammate toolbox 中使用。
func NewCompleteTaskToolV2WithAfterComplete(
	board tasks.Board,
	afterComplete func(tasks.Task),
) v2.Tool {
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
		executeCompleteTaskWithAfterComplete(board, afterComplete),
	)
}
