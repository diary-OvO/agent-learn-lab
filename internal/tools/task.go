package tools

import (
	v2 "AgentLoop/internal/toolkit/v2"
	"context"
	"encoding/json"
	"errors"
	"strings"
)

type TaskArgs struct {
	Description string `json:"description"`
}

type TaskRunner interface {
	Run(ctx context.Context, description string) (string, error)
}

func executeTask(agent TaskRunner) func(context.Context, json.RawMessage) (string, error) {
	return func(ctx context.Context, arguments json.RawMessage) (string, error) {
		if agent == nil {
			return "", errors.New("task runner is nil")
		}

		var args TaskArgs
		if err := json.Unmarshal(arguments, &args); err != nil {
			return "", err
		}
		description := strings.TrimSpace(args.Description)
		if description == "" {
			return "", errors.New("empty description")
		}
		return agent.Run(ctx, description)
	}
}
func NewTaskToolV2(agent TaskRunner) v2.Tool {
	return v2.NewFunctionTool(
		"task",
		"Launch a subagent to handle a complex subtask. Returns only the final conclusion.",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"description": map[string]any{
					"type":        "string",
					"description": "A clear, self-contained task description for the subagent.",
				},
			},
			"required":             []string{"description"},
			"additionalProperties": false,
		},
		executeTask(agent),
	)
}
