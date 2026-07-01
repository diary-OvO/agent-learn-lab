package team

import (
	"AgentLoop/internal/openaiadapter"
	v2 "AgentLoop/internal/toolkit/v2"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/openai/openai-go/v3"
)

const (
	teammateMaxRounds        = 10
	teammateIdlePollInterval = time.Second
)

type ToolboxFactory func(agentName string) *v2.ToolBox

// Spawner 对标 Python active_teammates + spawn_teammate_thread。
//
// S15 使用 SpawnLimited：最多 10 轮后自然退出。
// S16 使用 SpawnPersistent：无工具调用时进入 inbox idle loop，等待后续协议或消息。
type Spawner struct {
	client     openai.Client
	model      string
	bus        *MessageBus
	newToolbox ToolboxFactory

	mu     sync.Mutex
	active map[string]bool
}

// NewSpawner 对标 Python active_teammates 初始化。
//
// 创建当前 Lead 使用的 teammate spawner。
func NewSpawner(
	client openai.Client,
	model string,
	bus *MessageBus,
	newToolbox ToolboxFactory,
) *Spawner {
	return &Spawner{
		client:     client,
		model:      model,
		bus:        bus,
		newToolbox: newToolbox,
		active:     make(map[string]bool),
	}
}

// SpawnLimited 对标 Python S15 spawn_teammate_thread。
//
// 教学版 teammate loop：后台启动，最多 10 轮，完成后向 Lead 发送最终 summary。
func (s *Spawner) SpawnLimited(
	ctx context.Context,
	name string,
	role string,
	initialPrompt string,
) (string, error) {
	name, role, initialPrompt, reserved, err := s.reserveTeammate(
		name,
		role,
		initialPrompt,
	)
	if err != nil {
		return "", err
	}
	if !reserved {
		return fmt.Sprintf("Teammate %q already exists", name), nil
	}

	system := teammateSystemPrompt(name, role, false)

	go s.runLimitedTeammate(ctx, name, system, initialPrompt)

	fmt.Printf(
		"  \033[36m[teammate] %s spawned as %s\033[0m\n",
		name,
		role,
	)

	return fmt.Sprintf("Teammate %q spawned as %s", name, role), nil
}

// SpawnPersistent 对标 Python S16 spawn_teammate_thread。
//
// 持续版 teammate loop：无工具调用时不自然退出，而是 idle 等待 inbox。
func (s *Spawner) SpawnPersistent(
	ctx context.Context,
	name string,
	role string,
	initialPrompt string,
) (string, error) {
	name, role, initialPrompt, reserved, err := s.reserveTeammate(
		name,
		role,
		initialPrompt,
	)
	if err != nil {
		return "", err
	}
	if !reserved {
		return fmt.Sprintf("Teammate %q already exists", name), nil
	}

	system := teammateSystemPrompt(name, role, true)

	go s.runPersistentTeammate(ctx, name, system, initialPrompt)

	fmt.Printf(
		"  \033[36m[teammate] %s spawned as %s (persistent)\033[0m\n",
		name,
		role,
	)

	return fmt.Sprintf("Teammate %q spawned as %s (persistent)", name, role), nil
}

// HasActive 对标 Python active_teammates 非空判断。
//
// 用于 Lead 侧只在所有 teammate 完成且 inbox/background 已清空后打印完成提示。
func (s *Spawner) HasActive() bool {
	if s == nil {
		return false
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	return len(s.active) > 0
}

func (s *Spawner) reserveTeammate(
	name string,
	role string,
	initialPrompt string,
) (string, string, string, bool, error) {
	if s == nil {
		return "", "", "", false, fmt.Errorf("teammate spawner is nil")
	}

	name = strings.TrimSpace(name)
	role = strings.TrimSpace(role)
	initialPrompt = strings.TrimSpace(initialPrompt)

	if name == "" {
		return "", "", "", false, fmt.Errorf("name is required")
	}
	if role == "" {
		return "", "", "", false, fmt.Errorf("role is required")
	}
	if initialPrompt == "" {
		return "", "", "", false, fmt.Errorf("prompt is required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.active[name] {
		return name, role, initialPrompt, false, nil
	}

	s.active[name] = true

	return name, role, initialPrompt, true, nil
}

func teammateSystemPrompt(name string, role string, persistent bool) string {
	prompt := fmt.Sprintf(
		"You are %q, a %s. Use tools to complete tasks. Send results via send_message to 'lead'.",
		name,
		role,
	)

	if persistent {
		prompt += " When asked for a plan, use submit_plan and wait for plan_approval_response before continuing. When shutdown_request arrives, stop gracefully."
	}

	return prompt
}

func (s *Spawner) markDone(name string) {
	s.mu.Lock()
	delete(s.active, name)
	s.mu.Unlock()
}

func (s *Spawner) runLimitedTeammate(
	ctx context.Context,
	name string,
	system string,
	initialPrompt string,
) {
	defer s.finishTeammate(name)

	toolbox, chatTools, err := s.teammateToolbox(name)
	if err != nil {
		_ = s.bus.Send(name, "lead", "Failed to initialize teammate tools: "+err.Error(), "error")
		return
	}

	messages := []openai.ChatCompletionMessageParamUnion{
		openai.UserMessage(initialPrompt),
	}

	for round := 0; round < teammateMaxRounds; round++ {
		inbox, err := s.bus.ReadInbox(name)
		if err == nil && len(inbox) > 0 {
			messages = appendInboxMessage(messages, inbox)
		}

		var usedTools bool

		messages, usedTools, err = s.runOneTeammateTurn(
			ctx,
			system,
			messages,
			toolbox,
			chatTools,
		)
		if err != nil {
			_ = s.bus.Send(name, "lead", "Teammate error: "+err.Error(), "error")
			return
		}

		if !usedTools {
			break
		}
	}

	s.sendSummary(name, messages)
}

func (s *Spawner) runPersistentTeammate(
	ctx context.Context,
	name string,
	system string,
	initialPrompt string,
) {
	defer s.finishTeammate(name)

	toolbox, chatTools, err := s.teammateToolbox(name)
	if err != nil {
		_ = s.bus.Send(name, "lead", "Failed to initialize teammate tools: "+err.Error(), "error")
		return
	}

	messages := []openai.ChatCompletionMessageParamUnion{
		openai.UserMessage(initialPrompt),
	}

	for {
		inbox, err := s.bus.ReadInbox(name)
		if err == nil && len(inbox) > 0 {
			var shouldRun bool
			var shouldStop bool

			messages, shouldRun, shouldStop = s.applyPersistentInbox(
				name,
				messages,
				inbox,
			)
			if shouldStop {
				break
			}
			if !shouldRun {
				continue
			}
		}

		var usedTools bool

		messages, usedTools, err = s.runOneTeammateTurn(
			ctx,
			system,
			messages,
			toolbox,
			chatTools,
		)
		if err != nil {
			_ = s.bus.Send(name, "lead", "Teammate error: "+err.Error(), "error")
			return
		}

		if usedTools {
			continue
		}

		var shouldContinue bool
		messages, shouldContinue = s.waitForPersistentInbox(ctx, name, messages)
		if !shouldContinue {
			break
		}
	}

	s.sendSummary(name, messages)
}

func (s *Spawner) teammateToolbox(
	name string,
) (*v2.ToolBox, []openai.ChatCompletionToolUnionParam, error) {
	if s.newToolbox == nil {
		return nil, nil, fmt.Errorf("toolbox factory is nil")
	}

	toolbox := s.newToolbox(name)
	if toolbox == nil {
		return nil, nil, fmt.Errorf("toolbox is nil")
	}

	chatTools, err := openaiadapter.ToChatCompletionToolsV2(toolbox.Schemas())
	if err != nil {
		return nil, nil, err
	}

	return toolbox, chatTools, nil
}

func (s *Spawner) runOneTeammateTurn(
	ctx context.Context,
	system string,
	messages []openai.ChatCompletionMessageParamUnion,
	toolbox *v2.ToolBox,
	chatTools []openai.ChatCompletionToolUnionParam,
) ([]openai.ChatCompletionMessageParamUnion, bool, error) {
	response, err := s.client.Chat.Completions.New(
		ctx,
		openai.ChatCompletionNewParams{
			Model: s.model,
			Messages: append(
				[]openai.ChatCompletionMessageParamUnion{
					openai.SystemMessage(system),
				},
				safeRecent(messages, 20)...,
			),
			Tools:               chatTools,
			MaxCompletionTokens: openai.Int(8000),
		},
	)
	if err != nil {
		return messages, false, err
	}

	if len(response.Choices) == 0 {
		return messages, false, fmt.Errorf("empty response")
	}

	msg := response.Choices[0].Message
	messages = append(messages, msg.ToParam())

	if len(msg.ToolCalls) == 0 {
		return messages, false, nil
	}

	for _, toolCall := range msg.ToolCalls {
		call := v2.ToolCall{
			Name:      toolCall.Function.Name,
			Arguments: json.RawMessage(toolCall.Function.Arguments),
		}

		result, err := toolbox.Execute(ctx, call)
		if err != nil {
			result = fmt.Sprintf(`{"error": %q}`, err.Error())
		}

		messages = append(
			messages,
			openai.ToolMessage(result, toolCall.ID),
		)
	}

	return messages, true, nil
}

func (s *Spawner) waitForPersistentInbox(
	ctx context.Context,
	name string,
	messages []openai.ChatCompletionMessageParamUnion,
) ([]openai.ChatCompletionMessageParamUnion, bool) {
	ticker := time.NewTicker(teammateIdlePollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return messages, false
		case <-ticker.C:
			inbox, err := s.bus.ReadInbox(name)
			if err != nil || len(inbox) == 0 {
				continue
			}

			var shouldRun bool
			var shouldStop bool

			messages, shouldRun, shouldStop = s.applyPersistentInbox(
				name,
				messages,
				inbox,
			)
			if shouldStop {
				return messages, false
			}
			if shouldRun {
				return messages, true
			}
		}
	}
}

func (s *Spawner) applyPersistentInbox(
	name string,
	messages []openai.ChatCompletionMessageParamUnion,
	inbox []Message,
) ([]openai.ChatCompletionMessageParamUnion, bool, bool) {
	normalMessages := make([]Message, 0, len(inbox))
	shouldRun := false

	for _, msg := range inbox {
		switch msg.Type {
		case "shutdown_request":
			reqID := MetaString(msg.Metadata, "request_id")

			_ = s.bus.SendWithMetadata(
				name,
				"lead",
				"Shutting down gracefully.",
				"shutdown_response",
				map[string]any{
					"request_id": reqID,
					"approve":    true,
				},
			)

			return messages, false, true

		case "plan_approval_response":
			reqID := MetaString(msg.Metadata, "request_id")
			approved := metaBool(msg.Metadata, "approve")
			status := "rejected"
			if approved {
				status = "approved"
			}

			feedback := strings.TrimSpace(msg.Content)
			if feedback == "" {
				feedback = status
			}

			messages = append(
				messages,
				openai.UserMessage(
					fmt.Sprintf(
						"[Plan %s]\nRequest: %s\nFeedback: %s",
						status,
						reqID,
						feedback,
					),
				),
			)

			shouldRun = true

		default:
			normalMessages = append(normalMessages, msg)
		}
	}

	if len(normalMessages) > 0 {
		messages = appendInboxMessage(messages, normalMessages)
		shouldRun = true
	}

	return messages, shouldRun, false
}

func (s *Spawner) finishTeammate(name string) {
	s.markDone(name)

	fmt.Printf(
		"  \033[32m[teammate] %s finished\033[0m\n",
		name,
	)
}

func (s *Spawner) sendSummary(
	name string,
	messages []openai.ChatCompletionMessageParamUnion,
) {
	summary := "Done."

	for i := len(messages) - 1; i >= 0; i-- {
		if openaiadapter.MessageRole(messages[i]) != "assistant" {
			continue
		}

		text := strings.TrimSpace(openaiadapter.MessageTextContent(messages[i]))
		if text != "" {
			summary = text
			break
		}
	}

	_ = s.bus.Send(name, "lead", summary, "result")
}

func appendInboxMessage(
	messages []openai.ChatCompletionMessageParamUnion,
	inbox []Message,
) []openai.ChatCompletionMessageParamUnion {
	raw, _ := json.Marshal(inbox)

	return append(
		messages,
		openai.UserMessage("[Inbox]\n"+string(raw)),
	)
}

func safeRecent(
	messages []openai.ChatCompletionMessageParamUnion,
	n int,
) []openai.ChatCompletionMessageParamUnion {
	if n <= 0 || len(messages) <= n {
		return openaiadapter.CloneMessages(messages)
	}

	return openaiadapter.CloneMessages(messages[len(messages)-n:])
}
