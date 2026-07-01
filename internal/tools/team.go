package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"AgentLoop/internal/team"
	v2 "AgentLoop/internal/toolkit/v2"
)

type SpawnTeammateArgs struct {
	Name   string `json:"name"`
	Role   string `json:"role"`
	Prompt string `json:"prompt"`
}

type SendMessageArgs struct {
	To      string `json:"to"`
	Content string `json:"content"`
}

type RequestShutdownArgs struct {
	Teammate string `json:"teammate"`
}

type RequestPlanArgs struct {
	Teammate string `json:"teammate"`
	Task     string `json:"task"`
}

type ReviewPlanArgs struct {
	RequestID string `json:"request_id"`
	Approve   bool   `json:"approve"`
	Feedback  string `json:"feedback"`
}

type SubmitPlanArgs struct {
	Plan string `json:"plan"`
}

// NewSpawnLimitedTeammateToolV2 对标 Python S15 spawn_teammate tool schema。
//
// 注册 Lead 使用的教学版 teammate 创建工具：最多 10 轮后自然退出。
func NewSpawnLimitedTeammateToolV2(spawner *team.Spawner) v2.Tool {
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
		executeSpawnLimitedTeammate(spawner),
	)
}

// executeSpawnLimitedTeammate 对标 Python S15 run_spawn_teammate。
//
// 解析工具参数并调用 Spawner 启动 10 轮自然退出的 teammate goroutine。
func executeSpawnLimitedTeammate(
	spawner *team.Spawner,
) func(context.Context, json.RawMessage) (string, error) {
	return func(ctx context.Context, arguments json.RawMessage) (string, error) {
		if spawner == nil {
			return "", fmt.Errorf("teammate spawner is nil")
		}

		args, err := parseSpawnTeammateArgs(arguments)
		if err != nil {
			return "", err
		}

		return spawner.SpawnLimited(ctx, args.Name, args.Role, args.Prompt)
	}
}

// NewSpawnPersistentTeammateToolV2 对标 Python S16 spawn_teammate tool schema。
//
// 注册 Lead 使用的持续版 teammate 创建工具：无工具调用时 idle 等待 inbox。
func NewSpawnPersistentTeammateToolV2(spawner *team.Spawner) v2.Tool {
	return v2.NewFunctionTool(
		"spawn_teammate",
		"Spawn a teammate agent in a persistent background goroutine.",
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
		executeSpawnPersistentTeammate(spawner),
	)
}

// executeSpawnPersistentTeammate 对标 Python S16 run_spawn_teammate。
//
// 解析工具参数并调用 Spawner 启动持续等待 inbox 的 teammate goroutine。
func executeSpawnPersistentTeammate(
	spawner *team.Spawner,
) func(context.Context, json.RawMessage) (string, error) {
	return func(ctx context.Context, arguments json.RawMessage) (string, error) {
		if spawner == nil {
			return "", fmt.Errorf("teammate spawner is nil")
		}

		args, err := parseSpawnTeammateArgs(arguments)
		if err != nil {
			return "", err
		}

		return spawner.SpawnPersistent(ctx, args.Name, args.Role, args.Prompt)
	}
}

func parseSpawnTeammateArgs(arguments json.RawMessage) (SpawnTeammateArgs, error) {
	var args SpawnTeammateArgs
	if err := json.Unmarshal(arguments, &args); err != nil {
		return args, err
	}

	args.Name = strings.TrimSpace(args.Name)
	args.Role = strings.TrimSpace(args.Role)
	args.Prompt = strings.TrimSpace(args.Prompt)

	if args.Name == "" {
		return args, fmt.Errorf("name is required")
	}
	if args.Role == "" {
		return args, fmt.Errorf("role is required")
	}
	if args.Prompt == "" {
		return args, fmt.Errorf("prompt is required")
	}

	return args, nil
}

// NewSendMessageToolV2 对标 Python send_message tool schema。
//
// 注册向另一个 agent 发普通消息的工具。
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

// executeSendMessage 对标 Python run_send_message。
//
// 使用固定 fromAgent 向目标 agent inbox 写入普通 message。
func executeSendMessage(
	bus *team.MessageBus,
	fromAgent string,
) func(context.Context, json.RawMessage) (string, error) {
	return func(_ context.Context, arguments json.RawMessage) (string, error) {
		if bus == nil {
			return "", fmt.Errorf("message bus is nil")
		}

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

// NewCheckInboxToolV2 对标 Python run_check_inbox。
//
// Lead 手动读取 inbox；S16 传入 ProtocolBook 时会同步路由协议响应。
func NewCheckInboxToolV2(
	bus *team.MessageBus,
	book *team.ProtocolBook,
) v2.Tool {
	return v2.NewFunctionTool(
		"check_inbox",
		"Check Lead's inbox. Routes protocol responses automatically when protocol state is available.",
		map[string]any{
			"type":                 "object",
			"properties":           map[string]any{},
			"required":             []string{},
			"additionalProperties": false,
		},
		executeCheckInbox(bus, book),
	)
}

// executeCheckInbox 对标 Python consume_lead_inbox(route_protocol=True) + run_check_inbox。
//
// S16 Lead 使用 ProtocolBook 路由响应；S15/teammate 场景则只读取指定 agent inbox。
func executeCheckInbox(
	bus *team.MessageBus,
	book *team.ProtocolBook,
) func(context.Context, json.RawMessage) (string, error) {
	return func(_ context.Context, _ json.RawMessage) (string, error) {
		if bus == nil {
			return "(inbox empty)", nil
		}

		var (
			messages []team.Message
			err      error
		)

		if book != nil {
			messages, err = team.ConsumeLeadInbox(bus, book)
		} else {
			messages, err = bus.ReadInbox("lead")
		}
		if err != nil {
			return "", err
		}

		if len(messages) == 0 {
			return "(inbox empty)", nil
		}

		lines := make([]string, 0, len(messages))
		for _, msg := range messages {
			reqID := team.MetaString(msg.Metadata, "request_id")

			tag := "[" + msg.Type + "]"
			if reqID != "" {
				tag = fmt.Sprintf("[%s req:%s]", msg.Type, reqID)
			}

			lines = append(
				lines,
				fmt.Sprintf(
					"[%s] %s %s",
					msg.From,
					tag,
					previewRunes(msg.Content, 200),
				),
			)
		}

		return strings.Join(lines, "\n"), nil
	}
}

// NewRequestShutdownToolV2 对标 Python request_shutdown tool schema。
//
// Lead 请求 teammate 优雅退出，并登记 shutdown pending request。
func NewRequestShutdownToolV2(
	bus *team.MessageBus,
	book *team.ProtocolBook,
) v2.Tool {
	return v2.NewFunctionTool(
		"request_shutdown",
		"Request a teammate to shut down gracefully.",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"teammate": map[string]any{
					"type":        "string",
					"description": "Target teammate name.",
				},
			},
			"required":             []string{"teammate"},
			"additionalProperties": false,
		},
		executeRequestShutdown(bus, book),
	)
}

// executeRequestShutdown 对标 Python run_request_shutdown。
//
// 创建 shutdown request_id，并通过 MessageBus 发给 teammate。
func executeRequestShutdown(
	bus *team.MessageBus,
	book *team.ProtocolBook,
) func(context.Context, json.RawMessage) (string, error) {
	return func(_ context.Context, arguments json.RawMessage) (string, error) {
		if bus == nil {
			return "", fmt.Errorf("message bus is nil")
		}
		if book == nil {
			return "", fmt.Errorf("protocol book is nil")
		}

		var args RequestShutdownArgs
		if err := json.Unmarshal(arguments, &args); err != nil {
			return "", err
		}

		return book.RequestShutdown(bus, args.Teammate)
	}
}

// NewRequestPlanToolV2 对标 Python request_plan tool schema。
//
// Lead 请求 teammate 先提交计划；真正的 plan_approval request 由 teammate submit_plan 创建。
func NewRequestPlanToolV2(
	bus *team.MessageBus,
	book *team.ProtocolBook,
) v2.Tool {
	return v2.NewFunctionTool(
		"request_plan",
		"Ask a teammate to submit a plan for review.",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"teammate": map[string]any{
					"type":        "string",
					"description": "Target teammate name.",
				},
				"task": map[string]any{
					"type":        "string",
					"description": "Task that should be planned before execution.",
				},
			},
			"required":             []string{"teammate", "task"},
			"additionalProperties": false,
		},
		executeRequestPlan(bus, book),
	)
}

// executeRequestPlan 对标 Python run_request_plan。
//
// 向 teammate 发送普通消息，请它用 submit_plan 提交计划。
func executeRequestPlan(
	bus *team.MessageBus,
	book *team.ProtocolBook,
) func(context.Context, json.RawMessage) (string, error) {
	return func(_ context.Context, arguments json.RawMessage) (string, error) {
		if bus == nil {
			return "", fmt.Errorf("message bus is nil")
		}
		if book == nil {
			return "", fmt.Errorf("protocol book is nil")
		}

		var args RequestPlanArgs
		if err := json.Unmarshal(arguments, &args); err != nil {
			return "", err
		}

		return book.RequestPlan(bus, args.Teammate, args.Task)
	}
}

// NewReviewPlanToolV2 对标 Python review_plan tool schema。
//
// Lead 根据 request_id 批准或拒绝 teammate 提交的计划。
func NewReviewPlanToolV2(
	bus *team.MessageBus,
	book *team.ProtocolBook,
) v2.Tool {
	return v2.NewFunctionTool(
		"review_plan",
		"Approve or reject a submitted plan by request_id.",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"request_id": map[string]any{
					"type":        "string",
					"description": "Plan approval request ID.",
				},
				"approve": map[string]any{
					"type":        "boolean",
					"description": "True to approve, false to reject.",
				},
				"feedback": map[string]any{
					"type":        "string",
					"description": "Optional feedback sent with the review.",
				},
			},
			"required":             []string{"request_id", "approve"},
			"additionalProperties": false,
		},
		executeReviewPlan(bus, book),
	)
}

// executeReviewPlan 对标 Python run_review_plan。
//
// 根据 request_id 更新 pending state，并把审批结果发回 teammate。
func executeReviewPlan(
	bus *team.MessageBus,
	book *team.ProtocolBook,
) func(context.Context, json.RawMessage) (string, error) {
	return func(_ context.Context, arguments json.RawMessage) (string, error) {
		if bus == nil {
			return "", fmt.Errorf("message bus is nil")
		}
		if book == nil {
			return "", fmt.Errorf("protocol book is nil")
		}

		var args ReviewPlanArgs
		if err := json.Unmarshal(arguments, &args); err != nil {
			return "", err
		}

		return book.ReviewPlan(bus, args.RequestID, args.Approve, args.Feedback)
	}
}

// NewSubmitPlanToolV2 对标 Python teammate submit_plan tool schema。
//
// 只给 teammate 使用：向 Lead 提交计划审批请求。
func NewSubmitPlanToolV2(
	bus *team.MessageBus,
	book *team.ProtocolBook,
	fromName string,
) v2.Tool {
	return v2.NewFunctionTool(
		"submit_plan",
		"Submit a plan for Lead approval.",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"plan": map[string]any{
					"type":        "string",
					"description": "Plan text for Lead review.",
				},
			},
			"required":             []string{"plan"},
			"additionalProperties": false,
		},
		executeSubmitPlan(bus, book, fromName),
	)
}

// executeSubmitPlan 对标 Python _teammate_submit_plan。
//
// teammate 创建 plan_approval request，并发送给 Lead 等待 review_plan。
func executeSubmitPlan(
	bus *team.MessageBus,
	book *team.ProtocolBook,
	fromName string,
) func(context.Context, json.RawMessage) (string, error) {
	return func(_ context.Context, arguments json.RawMessage) (string, error) {
		if bus == nil {
			return "", fmt.Errorf("message bus is nil")
		}
		if book == nil {
			return "", fmt.Errorf("protocol book is nil")
		}

		var args SubmitPlanArgs
		if err := json.Unmarshal(arguments, &args); err != nil {
			return "", err
		}

		return book.SubmitPlan(bus, fromName, args.Plan)
	}
}
