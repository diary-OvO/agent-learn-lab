package openai_model

import (
	"AgentLoop/00-mini_agent_loop/openai_model/agentui"
	"AgentLoop/00-mini_agent_loop/openai_model/hooks"
	"AgentLoop/00-mini_agent_loop/openai_model/tools"
	v2 "AgentLoop/00-mini_agent_loop/openai_model/tools/v2"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/openai/openai-go/v3"
)

type SubAgent struct {
	client openai.Client

	toolbox       *v2.ToolBox
	toolboxSchema []openai.ChatCompletionToolUnionParam
	hookBus       *hooks.HookBus

	nextID atomic.Int64
}

func NewSubAgent(
	client openai.Client,
	toolbox *v2.ToolBox,
	hookBus *hooks.HookBus,
) (*SubAgent, error) {
	if toolbox == nil {
		toolbox = defaultSubToolBox()
	}
	toolboxSchema, err := ToChatCompletionToolsV2(toolbox.Schemas())
	if err != nil {
		return nil, err
	}
	return &SubAgent{
		client:        client,
		toolbox:       toolbox,
		toolboxSchema: toolboxSchema,
		hookBus:       hookBus,
	}, nil
}
func defaultSubToolBox() *v2.ToolBox {
	return v2.NewToolBox(
		tools.NewWeatherToolV2(),
		tools.NewBashToolV2(),
		tools.NewReadFileToolV2(),
		tools.NewWriteFileToolV2(),
		tools.NewEditFileToolV2(),
		tools.NewGlobToolV2(),
	)
}

func (a *SubAgent) Run(ctx context.Context, description string) (string, error) {
	system := "你是一个子智能体。完成被分配的任务，然后只返回简洁总结。不要继续委派任务。"
	id := fmt.Sprintf("sub-%d", a.nextID.Add(1))
	parentScope := agentui.ScopeFromContext(ctx)

	subCtx := agentui.WithAgentScope(ctx, agentui.AgentScope{
		Name:  "sub",
		ID:    id,
		Depth: parentScope.Depth + 1,
	})

	start := time.Now()

	if a.hookBus != nil {
		a.hookBus.TriggerSubagentStart(ctx, hooks.SubagentStartContext{
			ID:          id,
			Description: description,
		})
	}

	messages := []openai.ChatCompletionMessageParamUnion{
		openai.SystemMessage(system),
		openai.UserMessage(description),
	}

	summary, err := a.loop(subCtx, messages, 30)
	if err != nil {
		if a.hookBus != nil {
			a.hookBus.TriggerSubagentError(ctx, hooks.SubagentErrorContext{
				ID:      id,
				Err:     err,
				Elapsed: time.Since(start),
			})
		}
		return "", err
	}

	if a.hookBus != nil {
		a.hookBus.TriggerSubagentDone(ctx, hooks.SubagentDoneContext{
			ID:      id,
			Summary: summary,
			Elapsed: time.Since(start),
		})
	}

	return summary, nil
}
func (a *SubAgent) loop(
	ctx context.Context,
	messages []openai.ChatCompletionMessageParamUnion,
	maxSteps int,
) (string, error) {
	params := openai.ChatCompletionNewParams{
		Model:    "deepseek-v4-pro",
		Messages: messages,
		Tools:    a.toolboxSchema,
	}

	client := a.client
	toolbox := a.toolbox

	toolCallCount := 0
	lastAssistantText := ""

	for step := 0; step < maxSteps; step++ {
		completion, err := client.Chat.Completions.New(ctx, params)
		if err != nil {
			return "", err
		}

		msg := completion.Choices[0].Message
		if strings.TrimSpace(msg.Content) != "" {
			lastAssistantText = msg.Content
		}

		messages = append(messages, msg.ToParam())
		params.Messages = messages

		if len(msg.ToolCalls) == 0 {
			if a.hookBus != nil {
				force := a.hookBus.TriggerStop(ctx, hooks.StopContext{
					MessageCount:  len(messages),
					ToolCallCount: toolCallCount,
				})

				if strings.TrimSpace(force) != "" {
					messages = append(messages, openai.UserMessage(force))
					params.Messages = messages
					continue
				}
			}

			return msg.Content, nil
		}

		for _, toolCall := range msg.ToolCalls {
			call := v2.ToolCall{
				Name:      toolCall.Function.Name,
				Arguments: json.RawMessage(toolCall.Function.Arguments),
			}

			toolCallCount++

			if a.hookBus != nil {
				blocked := a.hookBus.TriggerPreToolUse(ctx, call)
				if strings.TrimSpace(blocked) != "" {
					result := a.hookBus.TriggerPostToolUse(ctx, call, blocked)

					messages = append(
						messages,
						openai.ToolMessage(result, toolCall.ID),
					)

					continue
				}
			}

			result, err := toolbox.Execute(ctx, call)

			if err != nil {
				result = fmt.Sprintf(`{"error": %q}`, err.Error())
			}

			if a.hookBus != nil {
				result = a.hookBus.TriggerPostToolUse(ctx, call, result)
			}

			messages = append(
				messages,
				openai.ToolMessage(result, toolCall.ID),
			)
		}

		params.Messages = messages
	}

	if strings.TrimSpace(lastAssistantText) != "" {
		return lastAssistantText, nil
	}

	return "", fmt.Errorf("agent loop reached max steps")
}
