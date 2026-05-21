package openai_model

import (
	v1 "AgentLoop/mini_agent_loop/openai_model/tools/v1"
	v2 "AgentLoop/mini_agent_loop/openai_model/tools/v2"
	v3 "AgentLoop/mini_agent_loop/openai_model/tools/v3"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/responses"
	"github.com/openai/openai-go/v3/shared"
)

type toolSchema struct {
	Name        string
	Description string
	Parameters  map[string]any
}

// toFunctionParameters 把通用 map 形式的 JSON Schema 转成 openai_model-go
// 需要的 FunctionParameters。
//
// 这里采用“先 marshal、再 unmarshal”的方式，避免手工拼装各种嵌套结构。
func toFunctionParameters(schema map[string]any) (shared.FunctionParameters, error) {
	var params shared.FunctionParameters
	data, err := json.Marshal(schema)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &params); err != nil {
		return nil, err
	}
	if typ, ok := params["type"].(string); ok && typ == "object" {
		if _, exists := params["properties"]; !exists {
			params["properties"] = map[string]any{}
		}
	}
	return params, nil
}

// 排序
// 为了让请求稳定、便于调试和缓存命中，这里先按名字排序再输出。
func sortToolSchemas(schemas []toolSchema) []toolSchema {
	sorted := append([]toolSchema(nil), schemas...)

	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Name < sorted[j].Name
	})

	return sorted
}

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

func fromV2ToolSchemas(schemas []v2.ToolSchema) []toolSchema {
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

/* 贴一段借鉴来的代码实现 出自项目trpc-agent-go
我的写法已经兼容v3的最新写法用上ChatCompletionToolUnionParam
// convertTools 把内部 ToolSchema 列表翻译成 OpenAI function tool schema。
//
// 这就是“Tool 列表传给模型”这一步的核心实现。
// 为了让请求稳定、便于调试和缓存命中，这里先按名字排序再输出。
func convertTools(tools []ToolSchema) []openai_model.ChatCompletionToolParam {
	if len(tools) == 0 {
		return nil
	}

	sorted := append([]ToolSchema(nil), tools...)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Name < sorted[j].Name
	})

	out := make([]openai_model.ChatCompletionToolParam, 0, len(sorted))
	for _, tl := range sorted {
		params, err := toFunctionParameters(tl.Parameters)
		if err != nil {
			panic(fmt.Sprintf("tool schema for %s is invalid: %v", tl.Name, err))
		}
		out = append(out, openai_model.ChatCompletionToolParam{
			Function: openai_model.FunctionDefinitionParam{
				Name:        tl.Name,
				Description: openai_model.String(tl.Description),
				Parameters:  params,
			},
		})
	}
	return out
}
*/

// ToChatCompletionTools 把内部 ToolSchema 列表转换成 OpenAI Chat.Completions tools
func ToChatCompletionTools(schemas []v1.ToolSchema) ([]openai.ChatCompletionToolUnionParam, error) {
	return toChatCompletionTools(fromV1ToolSchemas(schemas))
}

// ToChatCompletionToolsV2 把 v2 ToolSchema 列表转换成 OpenAI Chat.Completions tools
func ToChatCompletionToolsV2(schemas []v2.ToolSchema) ([]openai.ChatCompletionToolUnionParam, error) {
	return toChatCompletionTools(fromV2ToolSchemas(schemas))
}

// ToChatCompletionToolsV3 把 v3 ToolSchema 列表转换成 OpenAI Chat.Completions tools
func ToChatCompletionToolsV3(schemas []v3.ToolSchema) ([]openai.ChatCompletionToolUnionParam, error) {
	return toChatCompletionTools(fromV3ToolSchemas(schemas))
}

func toChatCompletionTools(schemas []toolSchema) ([]openai.ChatCompletionToolUnionParam, error) {
	if len(schemas) == 0 {
		return nil, nil
	}
	sorted := sortToolSchemas(schemas)

	out := make([]openai.ChatCompletionToolUnionParam, 0, len(sorted))

	for _, tl := range sorted {
		params, err := toFunctionParameters(tl.Parameters)
		if err != nil {
			return nil, fmt.Errorf("tool schema for %s is invalid: %w", tl.Name, err)
		}
		out = append(out, openai.ChatCompletionFunctionTool(
			openai.FunctionDefinitionParam{
				Name:        tl.Name,
				Description: openai.String(tl.Description),
				Parameters:  params,
			},
		))
	}

	return out, nil
}

// ToResponseTools 把内部 ToolSchema 列表转换成 OpenAI Responses tools
func ToResponseTools(schemas []v1.ToolSchema) ([]responses.ToolUnionParam, error) {
	return toResponseTools(fromV1ToolSchemas(schemas))
}

// ToResponseToolsV2 把 v2 ToolSchema 列表转换成 OpenAI Responses tools
func ToResponseToolsV2(schemas []v2.ToolSchema) ([]responses.ToolUnionParam, error) {
	return toResponseTools(fromV2ToolSchemas(schemas))
}

// ToResponseToolsV3 把 v3 ToolSchema 列表转换成 OpenAI Responses tools
func ToResponseToolsV3(schemas []v3.ToolSchema) ([]responses.ToolUnionParam, error) {
	return toResponseTools(fromV3ToolSchemas(schemas))
}

func toResponseTools(schemas []toolSchema) ([]responses.ToolUnionParam, error) {
	if len(schemas) == 0 {
		return nil, nil
	}
	sorted := sortToolSchemas(schemas)

	out := make([]responses.ToolUnionParam, 0, len(sorted))

	for _, tl := range sorted {
		params, err := toFunctionParameters(tl.Parameters)
		if err != nil {
			return nil, fmt.Errorf("tool schema for %s is invalid: %w", tl.Name, err)
		}
		out = append(out, responses.ToolUnionParam{
			OfFunction: &responses.FunctionToolParam{
				Name:        tl.Name,
				Description: openai.String(tl.Description),
				Parameters:  params,
			},
		})
	}
	return out, nil
}
