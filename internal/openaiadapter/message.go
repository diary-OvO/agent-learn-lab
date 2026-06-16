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

// MessageTextContent 对标 Python msg.get("content")。
//
// 从任意 ChatCompletion message 中提取可用于 compact / memory / prompt 拼接的文本内容。
func MessageTextContent(msg openai.ChatCompletionMessageParamUnion) string {
	switch {
	case msg.OfUser != nil && !param.IsOmitted(msg.OfUser):
		text, _ := UserTextContent(msg)
		return text

	case msg.OfAssistant != nil && !param.IsOmitted(msg.OfAssistant):
		text, _ := AssistantTextContent(msg)
		return text

	case msg.OfTool != nil && !param.IsOmitted(msg.OfTool):
		text, _ := ToolTextContent(msg)
		return text

	default:
		return messageContentText(msg.GetContent().AsAny())
	}
}

// MessageRoleAndText 对标 Python 同时读取 msg["role"] 和 msg["content"]。
//
// 为 compact / memory 这类课程流程提供统一的消息角色与文本提取入口。
func MessageRoleAndText(
	msg openai.ChatCompletionMessageParamUnion,
) (string, string) {
	return MessageRole(msg), MessageTextContent(msg)
}

// UserTextContent 对标 Python 从 user message 中读取 content 文本。
//
// 提取 user message 的可拼接文本；如果是多模态 parts，则合并 text 和必要占位符。
func UserTextContent(
	msg openai.ChatCompletionMessageParamUnion,
) (string, bool) {
	if msg.OfUser == nil || param.IsOmitted(msg.OfUser) {
		return "", false
	}

	return userContentText(msg.OfUser.Content)
}

// AssistantTextContent 对标 Python 从 assistant message 中读取 content 文本。
//
// 提取 assistant message 的可拼接文本，并兼容 refusal content part。
func AssistantTextContent(
	msg openai.ChatCompletionMessageParamUnion,
) (string, bool) {
	if msg.OfAssistant == nil || param.IsOmitted(msg.OfAssistant) {
		return "", false
	}

	text, ok := assistantContentText(msg.OfAssistant.Content)
	if ok {
		return text, true
	}

	if msg.OfAssistant.Refusal.Valid() {
		return msg.OfAssistant.Refusal.Value, true
	}

	return "", false
}

// ToolTextContent 对标 Python tool_result["content"]。
//
// 提取 tool message 的文本内容，兼容字符串和 text parts 两种 OpenAI content 形态。
func ToolTextContent(
	msg openai.ChatCompletionMessageParamUnion,
) (string, bool) {
	if msg.OfTool == nil || param.IsOmitted(msg.OfTool) {
		return "", false
	}

	return toolContentText(msg.OfTool.Content)
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

func messageContentText(content any) string {
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

func userContentText(
	content openai.ChatCompletionUserMessageParamContentUnion,
) (string, bool) {
	if !param.IsOmitted(content.OfString) {
		if !content.OfString.Valid() {
			return "", false
		}

		return content.OfString.Value, true
	}

	if !param.IsOmitted(content.OfArrayOfContentParts) {
		return userPartsText(content.OfArrayOfContentParts), true
	}

	return "", false
}

func assistantContentText(
	content openai.ChatCompletionAssistantMessageParamContentUnion,
) (string, bool) {
	if !param.IsOmitted(content.OfString) {
		if !content.OfString.Valid() {
			return "", false
		}

		return content.OfString.Value, true
	}

	if !param.IsOmitted(content.OfArrayOfContentParts) {
		return assistantPartsText(content.OfArrayOfContentParts), true
	}

	return "", false
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
