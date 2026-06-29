package prompt

import (
	"AgentLoop/internal/memory"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// PromptContext 对标 Python update_context 返回的 context dict。
//
// 它只保存组装 system prompt 所需的真实状态：工作区、已启用工具、Skill 索引、Memory 索引。
type PromptContext struct {
	WorkDir      string   `json:"workspace"`
	EnabledTools []string `json:"enabled_tools"`
	SkillCatalog string   `json:"skill_catalog"`
	Memories     string   `json:"memories"`
}

// Cache 对标 Python _last_context_key / _last_prompt。
//
// 它只缓存上一份 context 对应的 system prompt，避免同一进程内重复字符串组装；不是独立提示词管理器。
type Cache struct {
	lastContextKey string
	lastPrompt     string
}

// promptSections 对标 Python PROMPT_SECTIONS。
//
// Go 端用固定 key 保存提示词片段，但 Assemble 会显式按稳定顺序拼接，避免 map 迭代顺序影响 prompt。
var promptSections = map[string]string{
	"identity": "你是一个智能体猫猫娘 coding agent。Act, don't explain. 能用工具验证就验证，直接给结果，不输出内部思考。",

	"tools": "可用工具：%s",

	"workspace": "当前工作区：%s",

	"skills": "可用 Skills：\n%s\n\n当任务需要某个 Skill 的完整说明时，使用 load_skill 工具按 name 加载完整 SKILL.md。不要把完整 Skill 内容提前假设进回答；需要时再加载。",

	"memory": "Memories available:\n%s\n\nRelevant memories 会被临时注入到当前请求中。你必须尊重 memory 中记录的用户偏好、反馈、项目事实和参考信息。当用户说“记住”、表达稳定偏好、给出长期约束或项目事实时，回合结束后应提取为 memory。",

	"agent_loop": "在开始任何多步骤任务前，必须先使用 todo_write 规划步骤。遇到复杂子问题、需要上下文隔离或独立调查时，优先使用 task 工具启动子智能体，并只接收其最终结论。执行过程中持续更新 todo_write 的状态：开始做某一步前标记为 in_progress，完成后标记为 completed。",

	"compact": "当上下文过长、历史重复、工具输出过大，或需要释放上下文空间继续任务时，可以调用 compact 工具。compact 会保留当前目标、关键发现、文件变更、用户约束和剩余工作。",

	"permission": "你可以使用 Bash 和文件工具完成任务。所有破坏性操作都需要用户批准。",

	"style": "回答时保持可爱但专业的猫猫娘语气，按状态少量使用 Emoji（如 🐾执行中、✅完成、⚠️注意、❌失败、📌总结）。",
}

/*
 */
func UpdateContext(
	workDir string,
	tools []string,
	skills string,
	library memory.Library,
) (PromptContext, error) {
	memories, err := library.ReadIndex()
	if err != nil {
		return PromptContext{}, err
	}
	enabledTools := append([]string(nil), tools...)
	sort.Strings(enabledTools)

	return PromptContext{
		WorkDir:      workDir,
		EnabledTools: enabledTools,
		SkillCatalog: strings.TrimSpace(skills),
		Memories:     memories,
	}, nil
}

// Assemble 对标 Python assemble_system_prompt。
//
// 根据当前 PromptContext 选择并按稳定顺序拼接 system prompt 片段。
func Assemble(ctx PromptContext) string {
	sections := make([]string, 0, 8)
	sections = append(sections, promptSections["identity"])

	if len(ctx.EnabledTools) > 0 {
		sections = append(sections, fmt.Sprintf(
			promptSections["tools"],
			strings.Join(ctx.EnabledTools, ", "),
		))
	}

	sections = append(sections, fmt.Sprintf(promptSections["workspace"], ctx.WorkDir))

	if strings.TrimSpace(ctx.SkillCatalog) != "" {
		sections = append(sections, fmt.Sprintf(promptSections["skills"], ctx.SkillCatalog))
	}

	if strings.TrimSpace(ctx.Memories) != "" {
		sections = append(sections, fmt.Sprintf(promptSections["memory"], ctx.Memories))
	}

	sections = append(sections, promptSections["agent_loop"])
	sections = append(sections, promptSections["compact"])
	sections = append(sections, promptSections["permission"])
	sections = append(sections, promptSections["style"])

	return strings.Join(sections, "\n\n")
}

// Get 对标 Python get_system_prompt。
//
// 使用 json.Marshal 后的确定性 context key 做缓存；context 不变时复用上一份 prompt。
func (c *Cache) Get(ctx PromptContext) string {
	key := contextKey(ctx)
	if key == c.lastContextKey && c.lastPrompt != "" {
		fmt.Println(" \033[90m[cache hit] system prompt unchanged\033[0m")
		return c.lastPrompt
	}
	c.lastContextKey = key
	c.lastPrompt = Assemble(ctx)

	loaded := []string{"identity"}

	if len(ctx.EnabledTools) > 0 {
		loaded = append(loaded, "tools")
	}

	loaded = append(loaded, "workspace")
	if strings.TrimSpace(ctx.SkillCatalog) != "" {
		loaded = append(loaded, "skills")
	}
	if strings.TrimSpace(ctx.Memories) != "" {
		loaded = append(loaded, "memory")
	}
	loaded = append(loaded, "agent_loop", "compact", "permission", "style")

	fmt.Printf(" \033[32m[assembled] sections: %s\033[0m\n", strings.Join(loaded, ", "))

	return c.lastPrompt
}
func contextKey(ctx PromptContext) string {
	b, err := json.Marshal(ctx)
	if err != nil {
		return fmt.Sprintf("%#v", ctx)
	}

	return string(b)
}
