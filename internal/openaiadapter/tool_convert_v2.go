package openaiadapter

import (
	v2 "AgentLoop/internal/toolkit/v2"
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

// toFunctionParameters 把通用 map 形式的 JSON Schema 转成 openai-go
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

/* 贴一段借鉴来的代码实现 出自项目trpc-agent-go
我的写法已经兼容v3的最新写法用上ChatCompletionToolUnionParam
// convertTools 把内部 ToolSchema 列表翻译成 OpenAI function tool schema。
//
// 这就是“Tool 列表传给模型”这一步的核心实现。
// 为了让请求稳定、便于调试和缓存命中，这里先按名字排序再输出。
func convertTools(tools []ToolSchema) []openai.ChatCompletionToolParam {
	if len(tools) == 0 {
		return nil
	}

	sorted := append([]ToolSchema(nil), tools...)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Name < sorted[j].Name
	})

	out := make([]openai.ChatCompletionToolParam, 0, len(sorted))
	for _, tl := range sorted {
		params, err := toFunctionParameters(tl.Parameters)
		if err != nil {
			panic(fmt.Sprintf("tool schema for %s is invalid: %v", tl.Name, err))
		}
		out = append(out, openai.ChatCompletionToolParam{
			Function: openai.FunctionDefinitionParam{
				Name:        tl.Name,
				Description: openai.String(tl.Description),
				Parameters:  params,
			},
		})
	}
	return out
}
*/

// ToChatCompletionToolsV2 把 v2 ToolSchema 列表转换成 OpenAI Chat.Completions tools
func ToChatCompletionToolsV2(schemas []v2.ToolSchema) ([]openai.ChatCompletionToolUnionParam, error) {
	return toChatCompletionTools(fromV2ToolSchemas(schemas))
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

// ToResponseToolsV2 把 v2 ToolSchema 列表转换成 OpenAI Responses tools
func ToResponseToolsV2(schemas []v2.ToolSchema) ([]responses.ToolUnionParam, error) {
	return toResponseTools(fromV2ToolSchemas(schemas))
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
