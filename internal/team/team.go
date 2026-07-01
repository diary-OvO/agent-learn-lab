package team

import (
	"AgentLoop/internal/openaiadapter"
	v2 "AgentLoop/internal/toolkit/v2"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/openai/openai-go/v3"
)

const teammateMaxRounds = 10

type ToolboxFactory func(agentName string) *v2.ToolBox

// Spawner 对标 Python active_teammates + spawn_teammate_thread。
//
// 它只保存 teammate 启动所需的稳定状态，并用 goroutine 跑教学版 teammate loop。
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

// Spawn 对标 Python spawn_teammate_thread。
//
// 如果 teammate name 未被占用，则后台启动一个最多 10 轮的简化 agent loop。
func (s *Spawner) Spawn(
	ctx context.Context,
	name string,
	role string,
	initialPrompt string,
) (string, error) {
	if s == nil {
		return "", fmt.Errorf("teammate spawner is nil")
	}

	name = strings.TrimSpace(name)
	role = strings.TrimSpace(role)
	initialPrompt = strings.TrimSpace(initialPrompt)

	if name == "" {
		return "", fmt.Errorf("name is required")
	}
	if role == "" {
		return "", fmt.Errorf("role is required")
	}
	if initialPrompt == "" {
		return "", fmt.Errorf("prompt is required")
	}

	s.mu.Lock()
	if s.active[name] {
		s.mu.Unlock()
		return fmt.Sprintf("Teammate %q already exists", name), nil
	}

	s.active[name] = true
	s.mu.Unlock()

	system := fmt.Sprintf(
		"You are %q, a %s. Use tools to complete tasks. Send results via send_message to 'lead'.",
		name,
		role,
	)

	go s.runTeammate(ctx, name, system, initialPrompt)

	fmt.Printf(
		"  \033[36m[teammate] %s spawned as %s\033[0m\n",
		name,
		role,
	)

	return fmt.Sprintf("Teammate %q spawned as %s", name, role), nil
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

func (s *Spawner) markDone(name string) {
	s.mu.Lock()
	delete(s.active, name)
	s.mu.Unlock()
}

func (s *Spawner) runTeammate(
	ctx context.Context,
	name string,
	system string,
	initialPrompt string,
) {
	defer func() {
		s.markDone(name)

		fmt.Printf(
			"  \033[32m[teammate] %s finished\033[0m\n",
			name,
		)
	}()

	if s.newToolbox == nil {
		_ = s.bus.Send(name, "lead", "Failed to initialize teammate tools: toolbox factory is nil", "error")
		return
	}

	toolbox := s.newToolbox(name)
	if toolbox == nil {
		_ = s.bus.Send(name, "lead", "Failed to initialize teammate tools: toolbox is nil", "error")
		return
	}

	chatTools, err := openaiadapter.ToChatCompletionToolsV2(toolbox.Schemas())
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
			raw, _ := json.Marshal(inbox)

			messages = append(
				messages,
				openai.UserMessage("[Inbox]\n"+string(raw)),
			)
		}

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
			_ = s.bus.Send(name, "lead", "Teammate error: "+err.Error(), "error")
			return
		}

		if len(response.Choices) == 0 {
			_ = s.bus.Send(name, "lead", "Teammate stopped: empty response", "error")
			return
		}

		msg := response.Choices[0].Message
		messages = append(messages, msg.ToParam())

		if len(msg.ToolCalls) == 0 {
			break
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
	}

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

func safeRecent(
	messages []openai.ChatCompletionMessageParamUnion,
	n int,
) []openai.ChatCompletionMessageParamUnion {
	if n <= 0 || len(messages) <= n {
		return openaiadapter.CloneMessages(messages)
	}

	return openaiadapter.CloneMessages(messages[len(messages)-n:])
}
