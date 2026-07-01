package tools

import (
	"AgentLoop/internal/team"
	v2 "AgentLoop/internal/toolkit/v2"
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// SpawnTeammateArgs 对标 Python run_spawn_teammate 的参数。
//
// Lead 通过该工具创建一个后台 teammate agent。
type SpawnTeammateArgs struct {
	Name   string `json:"name"`
	Role   string `json:"role"`
	Prompt string `json:"prompt"`
}

// SendMessageArgs 对标 Python run_send_message 的参数。
//
// Agent 通过该工具向另一个 agent 的 inbox 写入消息。
type SendMessageArgs struct {
	To      string `json:"to"`
	Content string `json:"content"`
}

// NewSpawnTeammateToolV2 对标 Python spawn_teammate tool schema。
//
// 注册 Lead 使用的 teammate 创建工具。
func NewSpawnTeammateToolV2(spawner *team.Spawner) v2.Tool {
	return v2.NewFunctionTool(
		"spawn_teammate",
		"Spawn a teammate agent in a background goroutine.",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"name": map[string]any{
					"type":        "string",
					"description": "Unique teammate name.",
				},
				"role": map[string]any{
					"type":        "string",
					"description": "Teammate role, such as researcher, tester, reviewer.",
				},
				"prompt": map[string]any{
					"type":        "string",
					"description": "Initial task prompt for the teammate.",
				},
			},
			"required":             []string{"name", "role", "prompt"},
			"additionalProperties": false,
		},
		executeSpawnTeammate(spawner),
	)
}

func executeSpawnTeammate(
	spawner *team.Spawner,
) func(context.Context, json.RawMessage) (string, error) {
	return func(
		ctx context.Context,
		arguments json.RawMessage,
	) (string, error) {
		var args SpawnTeammateArgs
		if err := json.Unmarshal(arguments, &args); err != nil {
			return "", err
		}

		args.Name = strings.TrimSpace(args.Name)
		args.Role = strings.TrimSpace(args.Role)
		args.Prompt = strings.TrimSpace(args.Prompt)

		if args.Name == "" {
			return "", fmt.Errorf("name is required")
		}
		if args.Role == "" {
			return "", fmt.Errorf("role is required")
		}
		if args.Prompt == "" {
			return "", fmt.Errorf("prompt is required")
		}

		return spawner.Spawn(ctx, args.Name, args.Role, args.Prompt)
	}
}

// NewSendMessageToolV2 对标 Python send_message tool schema。
//
// 注册向另一个 agent 发消息的工具；fromAgent 由工具创建方固定。
func NewSendMessageToolV2(bus *team.MessageBus, fromAgent string) v2.Tool {
	return v2.NewFunctionTool(
		"send_message",
		"Send a message to another agent via MessageBus.",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"to": map[string]any{
					"type":        "string",
					"description": "Target agent name.",
				},
				"content": map[string]any{
					"type":        "string",
					"description": "Message content.",
				},
			},
			"required":             []string{"to", "content"},
			"additionalProperties": false,
		},
		executeSendMessage(bus, fromAgent),
	)
}

func executeSendMessage(
	bus *team.MessageBus,
	fromAgent string,
) func(context.Context, json.RawMessage) (string, error) {
	return func(
		_ context.Context,
		arguments json.RawMessage,
	) (string, error) {
		var args SendMessageArgs
		if err := json.Unmarshal(arguments, &args); err != nil {
			return "", err
		}

		args.To = strings.TrimSpace(args.To)
		args.Content = strings.TrimSpace(args.Content)

		if args.To == "" {
			return "", fmt.Errorf("to is required")
		}
		if args.Content == "" {
			return "", fmt.Errorf("content is required")
		}

		if err := bus.Send(fromAgent, args.To, args.Content, "message"); err != nil {
			return "", err
		}

		return "Sent to " + args.To, nil
	}
}

// NewCheckInboxToolV2 对标 Python check_inbox tool schema。
//
// 注册 Lead 手动读取 inbox 的工具；读取后会消费 inbox。
func NewCheckInboxToolV2(bus *team.MessageBus, agent string) v2.Tool {
	return v2.NewFunctionTool(
		"check_inbox",
		"Check Lead's inbox for teammate messages.",
		map[string]any{
			"type":                 "object",
			"properties":           map[string]any{},
			"required":             []string{},
			"additionalProperties": false,
		},
		executeCheckInbox(bus, agent),
	)
}

func executeCheckInbox(
	bus *team.MessageBus,
	agent string,
) func(context.Context, json.RawMessage) (string, error) {
	return func(
		_ context.Context,
		_ json.RawMessage,
	) (string, error) {
		messages, err := bus.ReadInbox(agent)
		if err != nil {
			return "", err
		}

		if len(messages) == 0 {
			return "(inbox empty)", nil
		}

		lines := make([]string, 0, len(messages))

		for _, msg := range messages {
			lines = append(
				lines,
				fmt.Sprintf(
					"[%s] %s",
					msg.From,
					previewRunes(msg.Content, 200),
				),
			)
		}

		return strings.Join(lines, "\n"), nil
	}
}
