package tools

import (
	"AgentLoop/00-mini_agent_loop/openai_model"
	v2 "AgentLoop/00-mini_agent_loop/openai_model/tools/v2"
	"context"
	"encoding/json"
	"errors"
	"strings"
)

type TaskArgs struct {
	Description string `json:"description"`
}

func executeTask(agent *openai_model.SubAgent) func(context.Context, json.RawMessage) (string, error) {
	return func(ctx context.Context, arguments json.RawMessage) (string, error) {
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
func NewTaskToolV2(agent *openai_model.SubAgent) v2.Tool {
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
