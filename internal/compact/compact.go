// Package compact 实现了 S08 上下文压缩（Compact）教学模块。
//
// S08 在 S07 的基础上，引入了四层上下文压缩流水线：
//
//   L1: 切片压缩 (snip compact)
//       当对话轮次过多时，直接裁剪掉中间部分的旧消息。
//       特点：成本低、结构化，按完整消息进行删除。
//
//   L2: 微观压缩 (micro compact)
//       保留最新工具的完整输出，将较旧的工具输出替换为简短的占位符。
//       特点：成本低、局部化，确保当前工作区的可读性。
//
//   L3: 工具输出预算 (tool result budget)
//       将极大的工具输出持久化到磁盘，在活跃上下文中仅保留路径和简短预览。
//       特点：成本低，但必须在 L2 之前运行，否则大输出在保存前就会被 L2 替换。
//
//   L4: 历史压缩 (compact history)
//       若经过上述低成本预处理后上下文仍然过大，则保存文字记录并调用大模型进行总结。
//       特点：成本高，因为需要额外进行一次 LLM 调用。
//
// 实际执行顺序故意与 L 编号不同（遵循“先轻后重”原则）：
//
//     L3 预算 -> L1 切片 -> L2 微观 -> L4 自动总结
//
// 这与 S08 Python 原版课程一致：先运行低成本操作，最后才使用高成本的 LLM 总结；
// 只有在 API 因提示词过长（prompt-too-long）而拒绝请求时，才会触发响应式压缩。

package compact

import (
	v2 "AgentLoop/internal/toolkit/v2"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/packages/param"
)

var (
	CONTEXT_LIMIT     = 50000
	KEEP_RECENT       = 3
	PERSIST_THRESHOLD = 300000
)

func CompactToolSchema() v2.ToolSchema {
	return v2.ToolSchema{
		Name:        "compact",
		Description: "Summarize earlier conversation to free context space.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"focus": map[string]any{
					"type":        "string",
					"description": "Optional focus to preserve while compacting earlier conversation.",
				},
			},
		},
	}
}

func EstimateSize(messages []openai.ChatCompletionMessageParamUnion) int {
	b, err := json.Marshal(messages)
	if err != nil {
		return len(fmt.Sprintf("%#v", messages))
	}

	return len(b)
}

// L1 消息裁剪：保留开头控制消息和最近上下文，中间用占位消息替换。
func SnipCompact(
	messages []openai.ChatCompletionMessageParamUnion,
	maxMessages int,
) []openai.ChatCompletionMessageParamUnion {
	if len(messages) <= maxMessages {
		return cloneMessages(messages)
	}

	keepHead := 3

	// 计算可保留的头部边界，避免截断 assistant/tool 调用配对。
	headEnd := safeHeadEnd(messages, keepHead)
	keepTail := maxMessages - headEnd - 1
	if keepTail < 1 {
		keepTail = 1
	}
	// 计算尾部起点，优先保留最近的对话和工具结果。
	tailStart := len(messages) - keepTail
	if tailStart < headEnd {
		tailStart = headEnd
	}
	tailStart = safeTailStart(messages, tailStart, headEnd)
	// 统计实际裁掉的消息数量，没有裁掉则直接返回副本。
	snipped := tailStart - headEnd
	if snipped <= 0 {
		return cloneMessages(messages)
	}
	// 最终结构：头部消息 + 裁剪说明 + 最近尾部消息。
	out := make([]openai.ChatCompletionMessageParamUnion, 0, headEnd+1+len(messages)-tailStart)
	out = append(out, messages[:headEnd]...)
	out = append(out, openai.UserMessage(fmt.Sprintf("[snipped %d messages]", snipped)))
	out = append(out, messages[tailStart:]...)

	return out
}

// L2 工具结果压缩：压缩较旧的工具输出，只保留最近 KEEP_RECENT 条完整结果。
func MicroCompact(message []openai.ChatCompletionMessageParamUnion) []openai.ChatCompletionMessageParamUnion {
	out := cloneMessages(message)

	// 收集 tool message 位置，后续只处理较旧的工具结果。
	var toolIdxs []int
	for i, msg := range out {
		if isToolMessage(msg) {
			toolIdxs = append(toolIdxs, i)
		}
	}

	if len(toolIdxs) <= KEEP_RECENT {
		return out
	}
	cutoff := len(toolIdxs) - KEEP_RECENT

	// 保留 tool_call_id，只替换内容，确保 OpenAI 消息链仍合法。
	for _, idx := range toolIdxs[:cutoff] {
		content, ok := toolContent(out[idx])
		if !ok || len(content) <= 120 {
			continue
		}
		id := toolCallID(out[idx])
		if id == "" {
			continue
		}
		out[idx] = openai.ToolMessage("[Earlier tool result compacted. Re-run if needed.]", id)
	}
	return out
}

// L3 工具输出预算：末尾工具结果过大时，把超大输出落盘，只留路径和预览。
func ToolResultBudget(message []openai.ChatCompletionMessageParamUnion, workDir string, maxBytes int) ([]openai.ChatCompletionMessageParamUnion, error) {
	out := cloneMessages(message)
	if len(out) == 0 {
		return out, nil
	}

	// 只检查末尾连续 tool message，避免改动已经稳定的历史上下文。
	start := len(out)
	for start > 0 && isToolMessage(out[start-1]) {
		start--
	}
	if start == len(out) {
		return out, nil
	}

	type block struct {
		idx     int
		id      string
		content string
		size    int
	}
	var blocks []block
	total := 0

	for i := start; i < len(out); i++ {
		content, ok := toolContent(out[i])
		if !ok {
			continue
		}
		id := toolCallID(out[i])
		if id == "" {
			id = "unknown"
		}
		size := len(content)
		total += size
		blocks = append(blocks, block{i, id, content, size})

	}
	if total < maxBytes {
		return out, nil
	}
	// 优先处理最大的结果，尽快把总量压回预算内。
	sort.Slice(blocks, func(i, j int) bool {
		return blocks[i].size > blocks[j].size
	})
	for _, b := range blocks {
		if total <= maxBytes {
			break
		}
		if len(b.content) <= PERSIST_THRESHOLD {
			continue
		}
		replaced, err := PersistLargeOutput(b.id, b.content, workDir)
		if err != nil {
			return out, err
		}
		out[b.idx] = openai.ToolMessage(replaced, b.id)
		total += len(replaced) - len(b.content)
	}
	return out, nil
}

// PersistLargeOutput 将大体积工具输出写入文件，并返回上下文中的替代文本。
func PersistLargeOutput(
	toolUseID string,
	output string,
	workDir string,
) (string, error) {
	if strings.TrimSpace(toolUseID) == "" {
		toolUseID = "unknown"
	}
	dir := filepath.Join(workDir, ".task_outputs", "tool-results")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return output, err
	}
	path := filepath.Join(dir, toolUseID+".txt")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		if err := os.WriteFile(path, []byte(output), 0o600); err != nil {
			return output, err
		}
	}
	preview := previewRunes(output, 2000)
	return fmt.Sprintf("<persisted-output>\nFull: %s\nPreview:\n%s\n</persisted-output>", path, preview), nil

}

// L4 历史压缩：保存完整 transcript，再调用模型生成可继续工作的摘要。
func CompactHistory(
	ctx context.Context,
	client openai.Client,
	model string,
	workDir string,
	messages []openai.ChatCompletionMessageParamUnion,
) ([]openai.ChatCompletionMessageParamUnion, error) {
	transcriptPath, err := WriteTranscript(messages, workDir)
	if err != nil {
		return messages, err
	}
	fmt.Printf("[transcript saved: %s]\n", transcriptPath)
	summary, err := SummarizeHistory(ctx, client, model, messages)
	if err != nil {
		return messages, err
	}

	out := leadingControlMessages(messages)
	out = append(out, openai.UserMessage("[Compacted]\n\n"+summary))
	return out, nil
}

// WriteTranscript 将完整消息历史保存为 jsonl，方便压缩后回溯。
func WriteTranscript(
	messages []openai.ChatCompletionMessageParamUnion,
	workDir string,
) (string, error) {
	dir := filepath.Join(workDir, ".transcripts")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, fmt.Sprintf("transcript_%d.jsonl", time.Now().Unix()))
	f, err := os.Create(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	for _, msg := range messages {
		b, err := json.Marshal(msg)
		if err != nil {
			b = []byte(fmt.Sprintf("%#v", msg))
		}
		f.Write(append(b, '\n'))
	}
	return path, nil
}

// SummarizeHistory 调用模型总结旧上下文，保留目标、决策和剩余工作。
func SummarizeHistory(
	ctx context.Context,
	client openai.Client,
	model string,
	messages []openai.ChatCompletionMessageParamUnion,
) (string, error) {
	raw, err := json.Marshal(messages)
	if err != nil {
		raw = []byte(fmt.Sprintf("%#v", messages))
	}
	convo := firstNRunes(string(raw), 80000)

	prompt := "Summarize this coding-agent conversation so work can continue.\n" +
		"Preserve: 1. current goal, 2. key findings/decisions, " +
		"3. files read/changed, 4. remaining work, 5. user constraints.\n\n" + convo

	resp, err := client.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
		Model: model,
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.UserMessage(prompt),
		},
		MaxCompletionTokens: openai.Int(2000),
	})
	if err != nil {
		return "", err
	}
	if len(resp.Choices) == 0 {
		return "(empty summary)", nil
	}
	summary := strings.TrimSpace(resp.Choices[0].Message.Content)
	if summary == "" {
		return "(empty summary)", nil
	}
	return summary, nil
}

// ReactiveCompact 是请求已超上下文后的兜底压缩。
func ReactiveCompact(
	ctx context.Context,
	client openai.Client,
	model string,
	workDir string,
	messages []openai.ChatCompletionMessageParamUnion,
) ([]openai.ChatCompletionMessageParamUnion, error) {
	_, _ = WriteTranscript(messages, workDir)
	summary, err := SummarizeHistory(ctx, client, model, messages)
	if err != nil {
		return messages, err
	}
	out := leadingControlMessages(messages)
	out = append(out, openai.UserMessage("[Reactive compact]\n\n"+summary))
	out = append(out, safeRecentTail(messages, 5)...)
	return out, nil
}

func cloneMessages(
	messages []openai.ChatCompletionMessageParamUnion,
) []openai.ChatCompletionMessageParamUnion {
	out := make([]openai.ChatCompletionMessageParamUnion, len(messages))
	copy(out, messages)

	return out
}

/*
原有的roleOf设计如下，但是在实际的实现过程中发现openai自身的结构里面实现了自带的身份预设与结构实现

优化前的roleOf实现如下，使用原生的json.Marsual
// messageMap 用 JSON 视角读取 openai-go 的 message union。
// 这样避免依赖太多 SDK 内部字段，保持本节代码只服务 compact 学习。
func messageMap(
	msg openai.ChatCompletionMessageParamUnion,
) map[string]any {
	b, err := json.Marshal(msg)
	if err != nil {
		return nil
	}

	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return nil
	}

	return m
}

func roleOf(
	msg openai.ChatCompletionMessageParamUnion,
) string {
	m := messageMap(msg)
	if m == nil {
		return ""
	}

	role, _ := m["role"].(string)
	return role
}
func toolContent(msg openai.ChatCompletionMessageParamUnion) (string, bool) {
	if !isToolMessage(msg) {
		return "", false
	}

	m := messageMap(msg)
	if m == nil {
		return "", false
	}

	content, ok := m["content"]
	if !ok {
		return "", false
	}

	switch v := content.(type) {
	case string:
		return v, true
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return fmt.Sprintf("%#v", v), true
		}
		return string(b), true
	}
}

func assistantToolCallIDs(msg openai.ChatCompletionMessageParamUnion) []string {
	if roleOf(msg) != "assistant" {
		return nil
	}

	m := messageMap(msg)
	if m == nil {
		return nil
	}

	rawCalls, ok := m["tool_calls"].([]any)
	if !ok {
		return nil
	}

	var ids []string
	for _, raw := range rawCalls {
		call, ok := raw.(map[string]any)
		if !ok {
			continue
		}

		id, _ := call["id"].(string)
		if id != "" {
			ids = append(ids, id)
		}
	}

	return ids
}
*/

func roleOf(msg openai.ChatCompletionMessageParamUnion) string {
	switch {
	case !param.IsOmitted(msg.OfDeveloper):
		return "developer"
	case !param.IsOmitted(msg.OfSystem):
		return "system"
	case !param.IsOmitted(msg.OfUser):
		return "user"
	case !param.IsOmitted(msg.OfAssistant):
		return "assistant"
	case !param.IsOmitted(msg.OfTool):
		return "tool"
	case !param.IsOmitted(msg.OfFunction):
		return "function"
	default:
		return ""
	}
}

// isControlMessage 判断是否为需要固定保留在最前面的控制消息。
func isControlMessage(msg openai.ChatCompletionMessageParamUnion) bool {
	role := roleOf(msg)
	return role == "system" || role == "developer"
}

// isToolMessage 判断消息是否为工具返回。
func isToolMessage(msg openai.ChatCompletionMessageParamUnion) bool {
	return roleOf(msg) == "tool"
}

// toolCallID 读取 tool message 对应的 tool_call_id。
func toolCallID(msg openai.ChatCompletionMessageParamUnion) string {
	if msg.OfTool != nil {
		return msg.OfTool.ToolCallID
	}
	return ""
}

// leadingControlMessageEnd 返回开头连续控制消息的结束位置。
func leadingControlMessageEnd(
	messages []openai.ChatCompletionMessageParamUnion,
) int {
	i := 0
	for i < len(messages) && isControlMessage(messages[i]) {
		i++
	}

	return i
}

// leadingControlMessages 提取开头的 system/developer 消息副本。
func leadingControlMessages(
	messages []openai.ChatCompletionMessageParamUnion,
) []openai.ChatCompletionMessageParamUnion {
	end := leadingControlMessageEnd(messages)

	return cloneMessages(messages[:end])
}

// toolContent 提取 tool message 的文本内容，兼容字符串和文本数组两种形式。
func toolContent(
	msg openai.ChatCompletionMessageParamUnion,
) (string, bool) {
	if param.IsOmitted(msg.OfTool) || msg.OfTool == nil {
		return "", false
	}

	content := msg.OfTool.Content

	// ChatCompletionToolMessageParamContentUnion:
	//   OfString              param.Opt[string]
	//   OfArrayOfContentParts []ChatCompletionContentPartTextParam
	//
	// 这里判断的是 union 分支是否存在，而不是字符串是否为空。
	// 因为空字符串也是合法的 tool content。
	if !param.IsOmitted(content.OfString) {
		if !content.OfString.Valid() {
			return "", false
		}

		return content.OfString.Value, true
	}

	if !param.IsOmitted(content.OfArrayOfContentParts) {
		var b strings.Builder

		for _, part := range content.OfArrayOfContentParts {
			b.WriteString(part.Text)
		}

		return b.String(), true
	}

	return "", false
}

// assistantToolCallIDs 读取 assistant 发起的所有工具调用 ID。
func assistantToolCallIDs(
	msg openai.ChatCompletionMessageParamUnion,
) []string {
	if param.IsOmitted(msg.OfAssistant) || msg.OfAssistant == nil {
		return nil
	}

	if len(msg.OfAssistant.ToolCalls) == 0 {
		return nil
	}

	ids := make([]string, 0, len(msg.OfAssistant.ToolCalls))

	for _, call := range msg.OfAssistant.ToolCalls {
		id := call.GetID()
		if id == nil || *id == "" {
			continue
		}

		ids = append(ids, *id)
	}

	return ids
}

// safeHeadEnd 用于 snip_compact。
// 如果 head 内有 assistant tool_call，但对应 tool message 没被包含，
// 就把 head 截到 assistant 之前。
func safeHeadEnd(
	messages []openai.ChatCompletionMessageParamUnion,
	desired int,
) int {
	if desired > len(messages) {
		desired = len(messages)
	}
	if desired < 0 {
		desired = 0
	}

	end := desired

	for end > 0 {
		if danglingAt := firstDanglingAssistant(messages[:end]); danglingAt >= 0 {
			end = danglingAt
			continue
		}

		return end
	}

	return 0
}

// firstDanglingAssistant 查找缺少配套 tool message 的 assistant 消息。
func firstDanglingAssistant(
	messages []openai.ChatCompletionMessageParamUnion,
) int {
	for i, msg := range messages {
		ids := assistantToolCallIDs(msg)
		if len(ids) == 0 {
			continue
		}

		seen := map[string]bool{}

		for j := i + 1; j < len(messages); j++ {
			if !isToolMessage(messages[j]) {
				break
			}

			id := toolCallID(messages[j])
			if id != "" {
				seen[id] = true
			}
		}

		for _, id := range ids {
			if !seen[id] {
				return i
			}
		}
	}

	return -1
}

// safeTailStart 用于 snip_compact / reactive_compact。
// 如果 tail 从 tool message 开始，就向前扩展到对应 assistant。
func safeTailStart(
	messages []openai.ChatCompletionMessageParamUnion,
	start int,
	minStart int,
) int {
	if start < minStart {
		start = minStart
	}

	if start >= len(messages) {
		return start
	}

	for start > minStart && isToolMessage(messages[start]) {
		start--
	}

	return start
}

// safeRecentTail 返回最近 n 条消息，并尽量保持工具调用配对完整。
func safeRecentTail(
	messages []openai.ChatCompletionMessageParamUnion,
	n int,
) []openai.ChatCompletionMessageParamUnion {
	if n <= 0 || len(messages) == 0 {
		return nil
	}

	minStart := leadingControlMessageEnd(messages)

	start := len(messages) - n
	if start < minStart {
		start = minStart
	}

	start = safeTailStart(messages, start, minStart)

	if start >= len(messages) {
		return nil
	}

	return cloneMessages(messages[start:])
}

// firstNRunes 按 rune 截取前 n 个字符，避免截断中文。
func firstNRunes(s string, n int) string {
	if n <= 0 {
		return ""
	}

	r := []rune(s)
	if len(r) <= n {
		return s
	}

	return string(r[:n])
}

// previewRunes 生成上下文预览，按 rune 截断。
func previewRunes(s string, n int) string {
	if n <= 0 {
		return ""
	}

	r := []rune(s)
	if len(r) <= n {
		return s
	}

	return string(r[:n])
}
