package openaiadapter

import (
	v1 "AgentLoop/internal/toolkit/v1"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/responses"
)

func fromV1ToolSchemas(schemas []v1.ToolSchema) []toolSchema {
	if len(schemas) == 0 {
		return nil
	}

	out := make([]toolSchema, 0, len(schemas))
	for _, schema := range schemas {
		out = append(out, toolSchema{
			Name:        schema.Name,
			Description: schema.Description,
			Parameters:  schema.Parameters,
		})
	}
	return out
}

// ToChatCompletionTools 把内部 ToolSchema 列表转换成 OpenAI Chat.Completions tools
func ToChatCompletionTools(schemas []v1.ToolSchema) ([]openai.ChatCompletionToolUnionParam, error) {
	return toChatCompletionTools(fromV1ToolSchemas(schemas))
}

// ToResponseTools 把内部 ToolSchema 列表转换成 OpenAI Responses tools
func ToResponseTools(schemas []v1.ToolSchema) ([]responses.ToolUnionParam, error) {
	return toResponseTools(fromV1ToolSchemas(schemas))
}
