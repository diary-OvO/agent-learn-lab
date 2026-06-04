package tools

import (
	"AgentLoop/internal/skills"
	v2 "AgentLoop/internal/toolkit/v2"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

type LoadSkillArgs struct {
	Name string `json:"name"`
}

func executeLoadSkill(registry *skills.Registry) func(ctx context.Context, message json.RawMessage) (string, error) {
	return func(ctx context.Context, arguments json.RawMessage) (string, error) {
		var args LoadSkillArgs
		if err := json.Unmarshal(arguments, &args); err != nil {
			return "", err
		}

		name := strings.TrimSpace(args.Name)
		if name == "" {
			return "", errors.New("empty skill name")
		}

		content, ok := registry.Load(name)
		if !ok {
			return fmt.Sprintf("Skill not found: %s", name), nil
		}
		return content, nil
	}
}

func NewLoadSkillToolV2(registry *skills.Registry) v2.Tool {
	return v2.NewFunctionTool(
		"load_skill",
		"Load the full content of a skill by name.",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"name": map[string]any{
					"type":        "string",
					"description": "The skill name to load, for example: code-review, pdf, agent-builder.",
				},
			},
			"required":             []string{"name"},
			"additionalProperties": false,
		},
		executeLoadSkill(registry),
	)
}
