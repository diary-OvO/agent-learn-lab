package tools

import (
	"AgentLoop/internal/tasks"
	v2 "AgentLoop/internal/toolkit/v2"
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// executeListTasks 对标 Python run_list_tasks。
//
// 读取全部持久化任务，并渲染状态、owner 和 blockedBy。
func executeListTasks(
	board tasks.Board,
) func(context.Context, json.RawMessage) (string, error) {
	return func(
		_ context.Context,
		_ json.RawMessage,
	) (string, error) {
		allTasks, err := board.List()
		if err != nil {
			return "", err
		}

		if len(allTasks) == 0 {
			return "No tasks. Use create_task to add some.", nil
		}

		var b strings.Builder

		for _, task := range allTasks {
			icon := "?"

			switch task.Status {
			case tasks.StatusPending:
				icon = "○"

			case tasks.StatusInProgress:
				icon = "●"

			case tasks.StatusCompleted:
				icon = "✓"
			}

			owner := ""
			if task.Owner != nil &&
				strings.TrimSpace(*task.Owner) != "" {
				owner = fmt.Sprintf(" [%s]", *task.Owner)
			}

			dependencies := ""
			if len(task.BlockedBy) > 0 {
				dependencies = fmt.Sprintf(" (blockedBy: %s)", strings.Join(task.BlockedBy, ", "))
			}

			fmt.Fprintf(&b, "  %s %s: %s [%s]%s%s\n", icon, task.ID, task.Subject, task.Status, owner, dependencies)
		}

		return strings.TrimRight(b.String(), "\n"), nil
	}
}

// NewListTasksToolV2 对标 Python list_tasks tool schema。
//
// 注册列出全部持久化任务的工具。
func NewListTasksToolV2(board tasks.Board) v2.Tool {
	return v2.NewFunctionTool(
		"list_tasks",
		"List all tasks with status, owner, and dependencies.",
		map[string]any{
			"type":                 "object",
			"properties":           map[string]any{},
			"required":             []string{},
			"additionalProperties": false,
		},
		executeListTasks(board),
	)
}
