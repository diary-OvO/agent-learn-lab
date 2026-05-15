package openai

import (
	"encoding/json"

	"github.com/openai/openai-go/v3/shared"
)

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
