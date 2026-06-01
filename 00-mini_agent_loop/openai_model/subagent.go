package openai_model

import (
	"AgentLoop/00-mini_agent_loop/openai_model/agentui"
	"AgentLoop/00-mini_agent_loop/openai_model/hooks"
	"AgentLoop/00-mini_agent_loop/openai_model/tools"
	v2 "AgentLoop/00-mini_agent_loop/openai_model/tools/v2"
	"context"
	"encoding/json"
	"fmt"
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
	agentui.PrintSubagentStart(ctx, id, description)

	messages := []openai.ChatCompletionMessageParamUnion{
		openai.SystemMessage(system),
		openai.UserMessage(description),
	}

	summary, err := a.loop(subCtx, messages, 30)
	if err != nil {
		agentui.PrintSubagentError(ctx, id, err, time.Since(start))
		return "", err
	}

	agentui.PrintSubagentDone(ctx, id, summary, time.Since(start))
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

	for step := 0; step < maxSteps; step++ {
		completion, err := client.Chat.Completions.New(ctx, params)
		if err != nil {
			return "", err
		}

		msg := completion.Choices[0].Message
		messages = append(messages, msg.ToParam())
		params.Messages = messages

		if len(msg.ToolCalls) == 0 {
			return msg.Content, nil
		}

		for _, toolCall := range msg.ToolCalls {
			call := v2.ToolCall{
				Name:      toolCall.Function.Name,
				Arguments: json.RawMessage(toolCall.Function.Arguments),
			}

			agentui.PrintToolCall(ctx, call)

			result, err := toolbox.Execute(ctx, call)

			if err != nil {
				result = fmt.Sprintf(`{"error": %q}`, err.Error())
			}

			messages = append(
				messages,
				openai.ToolMessage(result, toolCall.ID),
			)
		}
		params.Messages = messages
	}
	return "Subagent stopped after 30 turns without final answer.", nil
}
