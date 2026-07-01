package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"AgentLoop/internal/tasks"
	v2 "AgentLoop/internal/toolkit/v2"
	"AgentLoop/internal/worktree"
)

type CreateWorktreeArgs struct {
	Name   string `json:"name"`
	TaskID string `json:"task_id,omitempty"`
}

type RemoveWorktreeArgs struct {
	Name           string `json:"name"`
	DiscardChanges bool   `json:"discard_changes,omitempty"`
}

type KeepWorktreeArgs struct {
	Name string `json:"name"`
}

// executeCreateWorktree 对标 Python S18 run_create_worktree。
//
// 迭代原因：S18 Lead 需要通过工具创建隔离 worktree，并可选把 task_id 绑定到 task.worktree。
//
// 与旧 task 工具差别：create_task 只创建任务；create_worktree 创建 git worktree，
// 绑定任务时也不 claim，让 S17/S18 autonomous teammate 继续按任务板流程认领。
func executeCreateWorktree(
	store *worktree.Store,
	board tasks.Board,
) func(context.Context, json.RawMessage) (string, error) {
	return func(_ context.Context, arguments json.RawMessage) (string, error) {
		if store == nil {
			return "", fmt.Errorf("worktree store is nil")
		}

		var args CreateWorktreeArgs
		if err := json.Unmarshal(arguments, &args); err != nil {
			return "", err
		}

		args.Name = strings.TrimSpace(args.Name)
		args.TaskID = strings.TrimSpace(args.TaskID)
		if args.Name == "" {
			return "", fmt.Errorf("name is required")
		}

		return store.Create(args.Name, args.TaskID, board)
	}
}

// NewCreateWorktreeToolV2 对标 Python S18 create_worktree tool schema。
//
// 注册 Lead 工具：创建 .worktrees/{name} 和 wt/{name} branch，并可选绑定任务。
func NewCreateWorktreeToolV2(
	store *worktree.Store,
	board tasks.Board,
) v2.Tool {
	return v2.NewFunctionTool(
		"create_worktree",
		"Create an isolated git worktree with its own branch. Optionally bind it to a task.",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"name": map[string]any{
					"type":        "string",
					"description": "Worktree name, using letters, digits, dots, underscores, or dashes.",
				},
				"task_id": map[string]any{
					"type":        "string",
					"description": "Optional task ID to bind to this worktree without claiming it.",
				},
			},
			"required":             []string{"name"},
			"additionalProperties": false,
		},
		executeCreateWorktree(store, board),
	)
}

// executeRemoveWorktree 对标 Python S18 run_remove_worktree。
//
// 迭代原因：S18 Lead 需要能清理隔离 worktree，但默认不能丢弃未提交/未推送变更。
//
// 与 keep_worktree 差别：remove_worktree 会删除目录和 wt/{name} branch；
// keep_worktree 只记录保留事件。
func executeRemoveWorktree(
	store *worktree.Store,
) func(context.Context, json.RawMessage) (string, error) {
	return func(_ context.Context, arguments json.RawMessage) (string, error) {
		if store == nil {
			return "", fmt.Errorf("worktree store is nil")
		}

		var args RemoveWorktreeArgs
		if err := json.Unmarshal(arguments, &args); err != nil {
			return "", err
		}

		args.Name = strings.TrimSpace(args.Name)
		if args.Name == "" {
			return "", fmt.Errorf("name is required")
		}

		return store.Remove(args.Name, args.DiscardChanges)
	}
}

// NewRemoveWorktreeToolV2 对标 Python S18 remove_worktree tool schema。
//
// 注册 Lead 工具：安全删除 worktree；默认拒绝删除有变更的 worktree。
func NewRemoveWorktreeToolV2(store *worktree.Store) v2.Tool {
	return v2.NewFunctionTool(
		"remove_worktree",
		"Remove a worktree. Refuses local changes unless discard_changes is true.",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"name": map[string]any{
					"type":        "string",
					"description": "Worktree name.",
				},
				"discard_changes": map[string]any{
					"type":        "boolean",
					"description": "Force removal even if local changes or unpushed commits exist.",
				},
			},
			"required":             []string{"name"},
			"additionalProperties": false,
		},
		executeRemoveWorktree(store),
	)
}

// executeKeepWorktree 对标 Python S18 run_keep_worktree。
//
// 迭代原因：S18 删除前如果发现变更，Lead 可以选择保留 worktree 给人工 review。
//
// 与 remove_worktree 差别：这里不执行 git remove，只写 keep 事件并保留 branch。
func executeKeepWorktree(
	store *worktree.Store,
) func(context.Context, json.RawMessage) (string, error) {
	return func(_ context.Context, arguments json.RawMessage) (string, error) {
		if store == nil {
			return "", fmt.Errorf("worktree store is nil")
		}

		var args KeepWorktreeArgs
		if err := json.Unmarshal(arguments, &args); err != nil {
			return "", err
		}

		args.Name = strings.TrimSpace(args.Name)
		if args.Name == "" {
			return "", fmt.Errorf("name is required")
		}

		return store.Keep(args.Name)
	}
}

// NewKeepWorktreeToolV2 对标 Python S18 keep_worktree tool schema。
//
// 注册 Lead 工具：保留 worktree 给人工 review，并记录 keep 事件。
func NewKeepWorktreeToolV2(store *worktree.Store) v2.Tool {
	return v2.NewFunctionTool(
		"keep_worktree",
		"Keep a worktree for manual review. The branch is preserved.",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"name": map[string]any{
					"type":        "string",
					"description": "Worktree name.",
				},
			},
			"required":             []string{"name"},
			"additionalProperties": false,
		},
		executeKeepWorktree(store),
	)
}
