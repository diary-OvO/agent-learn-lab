package openaiadapter

import (
	"strings"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/packages/param"
)

// MessageRole 对标 Python msg.get("role")。
//
// 根据 ChatCompletionMessageParamUnion 的实际分支推断消息角色，避免通过 JSON 反查 role。
func MessageRole(msg openai.ChatCompletionMessageParamUnion) string {
	switch {
	case msg.OfDeveloper != nil && !param.IsOmitted(msg.OfDeveloper):
		return "developer"
	case msg.OfSystem != nil && !param.IsOmitted(msg.OfSystem):
		return "system"
	case msg.OfUser != nil && !param.IsOmitted(msg.OfUser):
		return "user"
	case msg.OfAssistant != nil && !param.IsOmitted(msg.OfAssistant):
		return "assistant"
	case msg.OfTool != nil && !param.IsOmitted(msg.OfTool):
		return "tool"
	case msg.OfFunction != nil && !param.IsOmitted(msg.OfFunction):
		return "function"
	default:
		return ""
	}
}

// MessageText 对标 Python msg.get("content")。
//
// 从 OpenAI ChatCompletionMessageParamUnion 中提取可用于 compact / memory / prompt 拼接的文本内容。
func MessageText(msg openai.ChatCompletionMessageParamUnion) string {
	text := contentText(msg.GetContent().AsAny())
	if text != "" {
		return text
	}

	if msg.OfAssistant != nil && msg.OfAssistant.Refusal.Valid() {
		return msg.OfAssistant.Refusal.Value
	}

	return ""
}

// MessageRoleAndText 对标 Python 同时读取 msg["role"] 和 msg["content"]。
//
// 为 compact / memory 这类课程流程提供统一的消息角色与文本提取入口。
func MessageRoleAndText(
	msg openai.ChatCompletionMessageParamUnion,
) (string, string) {
	return MessageRole(msg), MessageText(msg)
}

// IsControlMessage 对标 Python 中保留 system / developer 消息的判断。
//
// 判断消息是否属于需要优先保留的控制消息。
func IsControlMessage(msg openai.ChatCompletionMessageParamUnion) bool {
	role := MessageRole(msg)

	return role == "system" || role == "developer"
}

// IsToolMessage 对标 Python role == "tool" 的判断。
//
// 判断当前消息是否是 tool result message。
func IsToolMessage(msg openai.ChatCompletionMessageParamUnion) bool {
	return MessageRole(msg) == "tool"
}

// ToolCallID 对标 Python tool message 的 tool_call_id 读取。
//
// 读取 tool message 对应的 tool_call_id，用于保持 assistant tool_call 与 tool result 的配对关系。
func ToolCallID(msg openai.ChatCompletionMessageParamUnion) string {
	if msg.OfTool == nil || param.IsOmitted(msg.OfTool) {
		return ""
	}

	return msg.OfTool.ToolCallID
}

// ToolContent 对标 Python tool_result["content"]。
//
// 提取 tool message 的文本内容，兼容字符串和 text parts 两种 OpenAI content 形态。
func ToolContent(
	msg openai.ChatCompletionMessageParamUnion,
) (string, bool) {
	if msg.OfTool == nil || param.IsOmitted(msg.OfTool) {
		return "", false
	}

	return toolContentText(msg.OfTool.Content)
}

// CloneMessages 对标 Python messages.copy()。
//
// 对 OpenAI messages 切片做浅拷贝，用于构造 request 副本，避免临时上下文污染真实历史。
func CloneMessages(
	messages []openai.ChatCompletionMessageParamUnion,
) []openai.ChatCompletionMessageParamUnion {
	out := make([]openai.ChatCompletionMessageParamUnion, len(messages))
	copy(out, messages)

	return out
}

func contentText(content any) string {
	switch v := content.(type) {
	case *string:
		if v == nil {
			return ""
		}

		return *v

	case *[]openai.ChatCompletionContentPartTextParam:
		if v == nil {
			return ""
		}

		return textPartsText(*v)

	case *[]openai.ChatCompletionContentPartUnionParam:
		if v == nil {
			return ""
		}

		return userPartsText(*v)

	case *[]openai.ChatCompletionAssistantMessageParamContentArrayOfContentPartUnion:
		if v == nil {
			return ""
		}

		return assistantPartsText(*v)

	default:
		return ""
	}
}

func toolContentText(
	content openai.ChatCompletionToolMessageParamContentUnion,
) (string, bool) {
	if !param.IsOmitted(content.OfString) {
		if !content.OfString.Valid() {
			return "", false
		}

		return content.OfString.Value, true
	}

	if !param.IsOmitted(content.OfArrayOfContentParts) {
		return textPartsText(content.OfArrayOfContentParts), true
	}

	return "", false
}

func textPartsText(parts []openai.ChatCompletionContentPartTextParam) string {
	var b strings.Builder

	for _, part := range parts {
		appendText(&b, part.Text)
	}

	return b.String()
}

func userPartsText(parts []openai.ChatCompletionContentPartUnionParam) string {
	var b strings.Builder

	for _, part := range parts {
		if text := part.GetText(); text != nil {
			appendText(&b, *text)
			continue
		}

		if file := part.GetFile(); file != nil {
			appendText(&b, fileLabel(file))
			continue
		}

		if part.GetImageURL() != nil {
			appendText(&b, "[image]")
			continue
		}

		if part.GetInputAudio() != nil {
			appendText(&b, "[audio]")
			continue
		}
	}

	return b.String()
}

func assistantPartsText(
	parts []openai.ChatCompletionAssistantMessageParamContentArrayOfContentPartUnion,
) string {
	var b strings.Builder

	for _, part := range parts {
		if text := part.GetText(); text != nil {
			appendText(&b, *text)
			continue
		}

		if refusal := part.GetRefusal(); refusal != nil {
			appendText(&b, *refusal)
			continue
		}
	}

	return b.String()
}

func appendText(b *strings.Builder, text string) {
	if text == "" {
		return
	}

	if b.Len() > 0 {
		b.WriteByte(' ')
	}

	b.WriteString(text)
}

func fileLabel(file *openai.ChatCompletionContentPartFileFileParam) string {
	if file == nil {
		return "[file]"
	}

	if file.Filename.Valid() && file.Filename.Value != "" {
		return "[file:" + file.Filename.Value + "]"
	}

	if file.FileID.Valid() && file.FileID.Value != "" {
		return "[file:" + file.FileID.Value + "]"
	}

	return "[file]"
}

/*
// messageRoleAndText 是 Go + OpenAI ChatCompletion 的必要适配。
// Python 的消息是 dict，可以 msg.get("role")；Go 端这里从最终 JSON 形态读取 role/content。
func messageRoleAndText(
	msg openai.ChatCompletionMessageParamUnion,
) (string, string) {
	b, err := json.Marshal(msg)
	if err != nil {
		return "", ""
	}

	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return "", ""
	}

	role, _ := m["role"].(string)

	switch content := m["content"].(type) {
	case string:
		return role, content

	case []any:
		parts := make([]string, 0)

		for _, item := range content {
			block, ok := item.(map[string]any)
			if !ok {
				continue
			}

			if text, ok := block["text"].(string); ok {
				parts = append(parts, text)
			}
		}

		return role, strings.Join(parts, " ")

	default:
		if content == nil {
			return role, ""
		}

		raw, err := json.Marshal(content)
		if err != nil {
			return role, fmt.Sprintf("%v", content)
		}

		return role, string(raw)
	}
}

*/
