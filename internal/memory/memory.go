package memory

import (
	"AgentLoop/internal/openaiadapter"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"github.com/openai/openai-go/v3"
)

const ConsolidateThreshold = 10

// Library 对标 Python MEMORY_DIR + MEMORY_INDEX。
//
// 它表示带索引的记忆库目录，负责读写 memory 文件并维护 MEMORY.md。
type Library struct {
	Dir   string
	Index string
}

type Memory struct {
	Filename    string
	Name        string
	Description string
	Type        string
	Body        string
}

type memoryJSON struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Description string `json:"description"`
	Body        string `json:"body"`
}

// NewLibrary 对标 Python MEMORY_DIR.mkdir(exist_ok=True)。
//
// 创建并返回当前工作区的记忆库。
func NewLibrary(workDir string) (Library, error) {
	dir := filepath.Join(workDir, ".memory")
	//确保文件夹存在
	if err := os.MkdirAll(dir, 0755); err != nil {
		return Library{}, err
	}
	return Library{
		Dir:   dir,
		Index: filepath.Join(dir, "MEMORY.md"),
	}, nil
}

// Write -> write_memory_file
func (l Library) Write(
	name string,
	memType string,
	description string,
	body string,
) (string, error) {

	if err := os.MkdirAll(l.Dir, 0755); err != nil {
		return "", err
	}
	slug := slugName(name)

	filename := slug + ".md"
	path := filepath.Join(l.Dir, filename)

	content := fmt.Sprintf(
		"---\nname: %s\ndescription: %s\ntype: %s\n---\n\n%s\n",
		name,
		description,
		memType,
		body,
	)

	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return "", err
	}

	if err := l.RebuildIndex(); err != nil {
		return "", err
	}

	return path, nil
}

// RebuildIndex 对标 Python _rebuild_index。
func (l Library) RebuildIndex() error {
	if err := os.MkdirAll(l.Dir, 0755); err != nil {
		return err
	}

	entries, err := os.ReadDir(l.Dir)
	if err != nil {
		return err
	}
	lines := make([]string, 0)

	for _, entry := range entries {
		if entry.IsDir() || entry.Name() == "MEMORY.md" || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		path := filepath.Join(l.Dir, entry.Name())

		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		meta, body := parseFrontmatter(string(raw))

		name := meta["name"]
		if name == "" {
			name = strings.TrimSuffix(entry.Name(), ".md")
		}

		desc := meta["description"]
		if desc == "" {
			desc = firstLine(body)
			desc = firstNRunes(desc, 80)

		}

		lines = append(lines, fmt.Sprintf("- [%s](%s) — %s", name, entry.Name(), desc))
	}
	content := ""
	if len(lines) > 0 {
		content = strings.Join(lines, "\n") + "\n"
	}
	return os.WriteFile(l.Index, []byte(content), 0644)
}

// ReadIndex 对标 Python read_memory_index。
// 这个索引适合每轮注入 system prompt，因为它很小。
// 返回值大概长这样：
// - [user-preference-tabs](user-preference-tabs.md) — 用户偏好：喜欢用 tabs 展示多视图
// - [project-agent-loop](project-agent-loop.md) — 项目事实：这是一个 Go 版 agent loop 学习项目
func (l Library) ReadIndex() (string, error) {
	raw, err := os.ReadFile(l.Index)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(string(raw)), nil
}

// ReadFile 对标 Python read_memory_file。
// 只允许读取 .memory 下的单个文件名，避免 filename 带路径逃逸。
func (l Library) ReadFile(filename string) (string, error) {
	filename = filepath.Base(filename)

	path := filepath.Join(l.Dir, filename)
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}

	return string(raw), nil
}

// List 对标 Python list_memory_files。
// 读取所有 memory markdown，并解析最小 frontmatter。
func (l Library) List() ([]Memory, error) {
	entries, err := os.ReadDir(l.Dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	memories := make([]Memory, 0)

	for _, entry := range entries {
		if entry.IsDir() || entry.Name() == "MEMORY.md" || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		path := filepath.Join(l.Dir, entry.Name())

		raw, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		meta, body := parseFrontmatter(string(raw))
		name := meta["name"]
		if name == "" {
			name = strings.TrimSuffix(entry.Name(), ".md")
		}

		memType := meta["type"]
		if memType == "" {
			memType = "user"
		}
		memories = append(memories, Memory{
			Filename:    entry.Name(),
			Name:        name,
			Description: meta["description"],
			Type:        memType,
			Body:        body,
		})
	}

	return memories, nil
}

// SelectRelevant 对标 Python select_relevant_memories。
// 先让 LLM 从 name/description catalog 中选相关 memory；失败时 fallback 到关键词匹配。
func SelectRelevant(
	ctx context.Context,
	client openai.Client,
	model string,
	library Library,
	messages []openai.ChatCompletionMessageParamUnion,
	maxItems int,
) ([]string, error) {
	files, err := library.List()
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, nil
	}

	if maxItems <= 0 {
		maxItems = 5
	}

	recent := recentUserText(messages, 3)
	recent = firstNRunes(recent, 2000)
	if strings.TrimSpace(recent) == "" {
		return nil, nil
	}

	catalogLines := make([]string, 0, len(files))
	for i, f := range files {
		catalogLines = append(catalogLines, fmt.Sprintf("%d: %s — %s", i, f.Name, f.Description))
	}

	// 拼接提示词：告知 AI 任务背景（结合最近对话和记忆库）并提出核心要求
	prompt := "Given the recent conversation and the memory catalog below, " +
		"select the indices of memories that are clearly relevant. " +
		// 严格限制输出格式：要求 AI 只能返回包含整数索引的 JSON 数组（例如 [0, 3]）
		"Return ONLY a JSON array of integers, e.g. [0, 3]. " +
		// 兜底规则：如果没有任何关联的记忆，则返回空的 JSON 数组 []
		"If none are relevant, return [].\n\n" +
		// 注入最近的对话内容
		"Recent conversation:\n" + recent + "\n\n" +
		// 注入待筛选的记忆库列表（将多行记忆通过换行符拼接成文本块）
		"Memory catalog:\n" + strings.Join(catalogLines, "\n")
	resp, err := client.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
		Model: model,
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.UserMessage(prompt),
		},
		MaxCompletionTokens: openai.Int(200),
	})
	if err == nil && len(resp.Choices) > 0 {
		text := strings.TrimSpace(resp.Choices[0].Message.Content)

		var indices []int
		if raw := jsonArrayText(text); raw != "" && json.Unmarshal([]byte(raw), &indices) == nil {
			selected := make([]string, 0, maxItems)

			for _, idx := range indices {
				if idx >= 0 && idx < len(files) {
					selected = append(selected, files[idx].Filename)
					if len(selected) >= maxItems {
						break
					}
				}
			}

			return selected, nil
		}
	}

	// Python 原课 fallback：keyword matching on name + description。
	keywords := make([]string, 0)
	for _, word := range strings.Fields(strings.ToLower(recent)) {
		word = strings.TrimFunc(word, func(r rune) bool {
			return !unicode.IsLetter(r) && !unicode.IsDigit(r)
		})

		if len([]rune(word)) > 3 {
			keywords = append(keywords, word)
		}
	}

	selected := make([]string, 0, maxItems)

	for _, f := range files {
		text := strings.ToLower(f.Name + " " + f.Description)

		for _, kw := range keywords {
			if strings.Contains(text, kw) {
				selected = append(selected, f.Filename)
				break
			}
		}

		if len(selected) >= maxItems {
			break
		}
	}

	return selected, nil
}

// Load 对标 Python load_memories。
// 返回字符串用于临时注入 request_messages，不写回真实历史。
func Load(
	ctx context.Context,
	client openai.Client,
	model string,
	library Library,
	messages []openai.ChatCompletionMessageParamUnion,
) (string, error) {
	selected, err := SelectRelevant(ctx, client, model, library, messages, 5)
	if err != nil {
		return "", err
	}
	if len(selected) == 0 {
		return "", nil
	}
	parts := []string{"<relevant_memories>"}

	for _, filename := range selected {
		content, err := library.ReadFile(filename)
		if err != nil {
			continue
		}
		if strings.TrimSpace(content) != "" {
			parts = append(parts, content)
		}
	}
	parts = append(parts, "</relevant_memories>")

	return strings.Join(parts, "\n\n"), nil
}

// Extract 对标 Python extract_memories。
// 在每个用户回合结束后，从压缩前的最近对话中抽取 user preference / feedback / project fact。
func Extract(
	ctx context.Context,
	client openai.Client,
	model string,
	library Library,
	messages []openai.ChatCompletionMessageParamUnion,
) (int, error) {
	start := len(messages) - 10
	if start < 0 {
		start = 0
	}
	dialogueParts := make([]string, 0)

	for _, msg := range messages[start:] {
		role, text := openaiadapter.MessageRoleAndText(msg)
		if role != "user" && role != "assistant" {
			continue
		}
		if strings.TrimSpace(text) != "" {
			dialogueParts = append(dialogueParts, fmt.Sprintf("%s: %s", role, text))
		}
	}

	dialogue := strings.Join(dialogueParts, "\n")
	if strings.TrimSpace(dialogue) == "" {
		return 0, nil
	}

	existing, err := library.List()
	if err != nil {
		return 0, err
	}

	existingDesc := "(none)"
	if len(existing) > 0 {
		lines := make([]string, 0, len(existing))

		for _, m := range existing {
			lines = append(lines, fmt.Sprintf("- %s: %s", m.Name, m.Description))
		}

		existingDesc = strings.Join(lines, "\n")
	}

	// 拼接提示词：要求 AI 从对话中提取用户的偏好、约束条件或项目事实
	prompt := "Extract user preferences, constraints, or project facts from this dialogue.\n" +
		// 限制输出格式：必须返回一个 JSON 数组，且规定了每个对象的 4 个固定字段
		"Return a JSON array. Each item: {name, type, description, body}.\n" +
		// 字段 1：name 必须是短的、用中划线连接的标识符（例如之前学到的 slugName 格式）
		"- name: short kebab-case identifier (e.g. 'user-preference-tabs')\n" +
		// 字段 2：type 必须是以下四种类型之一：用户偏好、指导反馈、项目事实或外部引用
		"- type: one of 'user' (user preference), 'feedback' (guidance), " +
		"'project' (project fact), 'reference' (external pointer)\n" +
		// 字段 3：description 是单行摘要，用于日后做索引检索
		"- description: one-line summary for index lookup\n" +
		// 字段 4：body 是完整的详细内容，使用 Markdown 格式编写
		"- body: full detail in markdown\n" +
		// 去重/过滤机制：如果没发现新信息，或者已被现有记忆覆盖，则直接返回空数组 []
		"If nothing new or already covered by existing memories, return [].\n\n" +
		// 注入已有的记忆库描述（防止 AI 提取出重复的信息）
		"Existing memories:\n" + existingDesc + "\n\n" +
		// 注入当前发生的对话正文（通过 firstNRunes 截取前 4000 个字符，防止超出模型上下文限制）
		"Dialogue:\n" + firstNRunes(dialogue, 4000)

	resp, err := client.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
		Model: model,
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.UserMessage(prompt),
		},
		MaxCompletionTokens: openai.Int(800),
	})
	if err != nil {
		return 0, err
	}
	if len(resp.Choices) == 0 {
		return 0, nil
	}

	raw := jsonArrayText(strings.TrimSpace(resp.Choices[0].Message.Content))
	if raw == "" {
		return 0, nil
	}

	var items []memoryJSON
	if err := json.Unmarshal([]byte(raw), &items); err != nil {
		return 0, nil
	}

	count := 0

	for _, item := range items {
		if strings.TrimSpace(item.Description) == "" || strings.TrimSpace(item.Body) == "" {
			continue
		}

		name := item.Name
		if strings.TrimSpace(name) == "" {
			name = fmt.Sprintf("memory-%d", time.Now().Unix())
		}

		memType := item.Type
		if strings.TrimSpace(memType) == "" {
			memType = "user"
		}

		if _, err := library.Write(name, memType, item.Description, item.Body); err != nil {
			return count, err
		}

		count++
	}

	if count > 0 {
		fmt.Printf("\n\033[33m[Memory: extracted %d new memories]\033[0m\n", count)
	}

	return count, nil
}

// Consolidate 对标 Python consolidate_memories。
// 达到阈值后，调用一次 LLM 合并重复/陈旧 memory。
func Consolidate(
	ctx context.Context,
	client openai.Client,
	model string,
	library Library,
) error {
	files, err := library.List()
	if err != nil {
		return err
	}

	if len(files) < ConsolidateThreshold {
		return nil
	}

	blocks := make([]string, 0, len(files))
	for _, f := range files {
		blocks = append(blocks, fmt.Sprintf(
			"## %s\nname: %s\ndescription: %s\n%s",
			f.Filename,
			f.Name,
			f.Description,
			f.Body,
		))
	}

	// 拼接提示词：要求 AI 对以下给出的记忆文件进行整合与固化（Consolidate）
	prompt := "Consolidate the following memory files. Rules:\n" +
		// 规则 1：将重复或高度相似的记忆合并为一条
		"1. Merge duplicates into one\n" +
		// 规则 2：删除已经过时的、或者前后相互矛盾的历史记忆
		"2. Remove outdated/contradicted memories\n" +
		// 规则 3：控制记忆库总体积，确保合并后的总记忆条数不超过 30 条
		"3. Keep the total under 30 memories\n" +
		// 规则 4：最高优先级——无论如何都要优先保留核心的用户偏好设置
		"4. Preserve important user preferences above all\n" +
		// 限制输出格式：依然严格要求返回结构化的 JSON 数组，包含规定的 4 个字段
		"Return a JSON array. Each item: {name, type, description, body}.\n\n" +
		// 注入待整理的所有记忆数据（将所有记忆块用双换行拼接，并截取前 16000 个字符以防 Token 溢出）
		firstNRunes(strings.Join(blocks, "\n\n"), 16000)

	resp, err := client.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
		Model: model,
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.UserMessage(prompt),
		},
		MaxCompletionTokens: openai.Int(3000),
	})
	if err != nil {
		return err
	}
	if len(resp.Choices) == 0 {
		return nil
	}

	raw := jsonArrayText(strings.TrimSpace(resp.Choices[0].Message.Content))
	if raw == "" {
		return nil
	}

	var items []memoryJSON
	if err := json.Unmarshal([]byte(raw), &items); err != nil {
		return nil
	}

	entries, err := os.ReadDir(library.Dir)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		if entry.IsDir() || entry.Name() == "MEMORY.md" || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}

		_ = os.Remove(filepath.Join(library.Dir, entry.Name()))
	}

	written := 0

	for _, item := range items {
		if strings.TrimSpace(item.Description) == "" || strings.TrimSpace(item.Body) == "" {
			continue
		}

		name := item.Name
		if strings.TrimSpace(name) == "" {
			name = fmt.Sprintf("memory-%d", time.Now().Unix())
		}

		memType := item.Type
		if strings.TrimSpace(memType) == "" {
			memType = "user"
		}

		if _, err := library.Write(name, memType, item.Description, item.Body); err != nil {
			return err
		}

		written++
	}

	if written == 0 {
		if err := library.RebuildIndex(); err != nil {
			return err
		}
	}

	fmt.Printf("\n\033[33m[Memory: consolidated %d → %d memories]\033[0m\n", len(files), written)

	return nil
}

// parseFrontmatter 对标 Python _parse_frontmatter。
// 这里只支持课程所需的最小 YAML frontmatter：key: value。
func parseFrontmatter(text string) (map[string]string, string) {
	if !strings.HasPrefix(text, "---") {
		return map[string]string{}, text
	}

	parts := strings.SplitN(text, "---", 3)
	if len(parts) < 3 {
		return map[string]string{}, text
	}

	meta := map[string]string{}

	for _, line := range strings.Split(strings.TrimSpace(parts[1]), "\n") {
		if !strings.Contains(line, ":") {
			continue
		}

		pair := strings.SplitN(line, ":", 2)
		key := strings.TrimSpace(pair[0])
		value := strings.Trim(strings.TrimSpace(pair[1]), `"'`)

		if key != "" {
			meta[key] = value
		}
	}

	return meta, strings.TrimSpace(parts[2])
}

// slugName 用作生成安全的文件名
func slugName(name string) string {
	name = strings.TrimSpace(strings.ToLower(name))
	var b strings.Builder
	for _, r := range name {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
		case r == '-' || r == '_':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	slug := strings.Trim(b.String(), "-_")
	if slug == "" {
		return fmt.Sprintf("memory-%d", time.Now().Unix())
	}
	return firstNRunes(slug, 120)

}

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
func firstLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			return line
		}
	}

	return ""
}

//倒序遍历聊天历史，抓取最近 N 条用户发言

func recentUserText(
	messages []openai.ChatCompletionMessageParamUnion,
	maxItems int,
) string {
	if maxItems <= 0 {
		maxItems = 3
	}

	parts := make([]string, 0, maxItems)

	for i := len(messages) - 1; i >= 0; i-- {
		role, text := openaiadapter.MessageRoleAndText(messages[i])
		if role != "user" || strings.TrimSpace(text) == "" {
			continue
		}

		parts = append([]string{text}, parts...)

		if len(parts) >= maxItems {
			break
		}
	}

	return strings.Join(parts, " ")
}
func jsonArrayText(s string) string {
	start := strings.Index(s, "[")
	end := strings.LastIndex(s, "]")

	if start < 0 || end <= start {
		return ""
	}

	return s[start : end+1]
}
