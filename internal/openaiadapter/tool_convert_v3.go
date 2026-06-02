package openaiadapter

import (
	v3 "AgentLoop/internal/toolkit/v3"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/responses"
)

func fromV3ToolSchemas(schemas []v3.ToolSchema) []toolSchema {
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

// ToChatCompletionToolsV3 把 v3 ToolSchema 列表转换成 OpenAI Chat.Completions tools
func ToChatCompletionToolsV3(schemas []v3.ToolSchema) ([]openai.ChatCompletionToolUnionParam, error) {
	return toChatCompletionTools(fromV3ToolSchemas(schemas))
}

// ToResponseToolsV3 把 v3 ToolSchema 列表转换成 OpenAI Responses tools
func ToResponseToolsV3(schemas []v3.ToolSchema) ([]responses.ToolUnionParam, error) {
	return toResponseTools(fromV3ToolSchemas(schemas))
}
