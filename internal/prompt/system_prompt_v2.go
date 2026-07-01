package prompt

import (
	"fmt"
	"strings"
)

// SectionV2 对标 Python PROMPT_SECTIONS 的单个 section。
//
// V2 不再依赖包级固定 map，而是由每个课程入口选择需要的 section 列表。
type SectionV2 struct {
	Key       string
	Signature string
	Build     func(PromptContext) (string, bool)
}

// CacheV2 对标 Python _last_context_key / _last_prompt。
//
// V2 把 section 列表也纳入缓存 key，允许不同课程使用不同 prompt 组合。
type CacheV2 struct {
	lastContextKey string
	lastPrompt     string
}

// StaticSectionV2 对标 Python PROMPT_SECTIONS["name"] = "..."。
//
// 创建一个始终注入的固定 section，用于 identity、agent_loop、compact 等稳定片段。
func StaticSectionV2(key string, text string) SectionV2 {
	return SectionV2{
		Key:       key,
		Signature: text,
		Build: func(PromptContext) (string, bool) {
			value := strings.TrimSpace(text)
			return value, value != ""
		},
	}
}

// ToolGatedSectionV2 创建一个受工具集合控制的固定 section。
//
// 课程可以把新增语义绑定到真实启用工具上，避免工具被裁剪后 prompt 仍描述不可用能力。
func ToolGatedSectionV2(key string, text string, toolNames ...string) SectionV2 {
	return SectionV2{
		Key:       key,
		Signature: text + "|tools:" + strings.Join(toolNames, ","),
		Build: func(ctx PromptContext) (string, bool) {
			value := strings.TrimSpace(text)
			if value == "" || !hasAnyEnabledToolV2(ctx.EnabledTools, toolNames) {
				return "", false
			}

			return value, true
		},
	}
}

// ToolsSectionV2 对标 Python PROMPT_SECTIONS["tools"]。
//
// 根据真实启用工具动态渲染工具列表；没有工具时跳过。
func ToolsSectionV2(text string) SectionV2 {
	return SectionV2{
		Key:       "tools",
		Signature: text,
		Build: func(ctx PromptContext) (string, bool) {
			if len(ctx.EnabledTools) == 0 {
				return "", false
			}

			return fmt.Sprintf(text, strings.Join(ctx.EnabledTools, ", ")), true
		},
	}
}

// WorkspaceSectionV2 对标 Python PROMPT_SECTIONS["workspace"]。
//
// 把当前工作区作为真实状态注入 system prompt。
func WorkspaceSectionV2(text string) SectionV2 {
	return SectionV2{
		Key:       "workspace",
		Signature: text,
		Build: func(ctx PromptContext) (string, bool) {
			workDir := strings.TrimSpace(ctx.WorkDir)
			if workDir == "" {
				return "", false
			}

			return fmt.Sprintf(text, workDir), true
		},
	}
}

// SkillsSectionV2 对标 Python PROMPT_SECTIONS["skills"]。
//
// 只有当前 Skill catalog 非空时才注入，避免空章节占用上下文。
func SkillsSectionV2(text string) SectionV2 {
	return SectionV2{
		Key:       "skills",
		Signature: text,
		Build: func(ctx PromptContext) (string, bool) {
			catalog := strings.TrimSpace(ctx.SkillCatalog)
			if catalog == "" {
				return "", false
			}

			return fmt.Sprintf(text, catalog), true
		},
	}
}

// MemorySectionV2 对标 Python PROMPT_SECTIONS["memory"]。
//
// 只有 memory index 有内容时才注入；具体相关记忆仍由请求副本临时拼接。
func MemorySectionV2(text string) SectionV2 {
	return SectionV2{
		Key:       "memory",
		Signature: text,
		Build: func(ctx PromptContext) (string, bool) {
			memories := strings.TrimSpace(ctx.Memories)
			if memories == "" {
				return "", false
			}

			return fmt.Sprintf(text, memories), true
		},
	}
}

// BaseSectionsV2 对标 S10 基础 PROMPT_SECTIONS。
//
// 返回一份可复制、可插入的基础 section 列表，后续课程在此基础上增量定制。
func BaseSectionsV2() []SectionV2 {
	return []SectionV2{
		StaticSectionV2(
			"identity",
			"你是一个智能体猫猫娘 coding agent。Act, don't explain. 能用工具验证就验证，直接给结果，不输出内部思考。",
		),
		ToolsSectionV2("可用工具：%s"),
		WorkspaceSectionV2("当前工作区：%s"),
		SkillsSectionV2("可用 Skills：\n%s\n\n当任务需要某个 Skill 的完整说明时，使用 load_skill 工具按 name 加载完整 SKILL.md。不要把完整 Skill 内容提前假设进回答；需要时再加载。"),
		MemorySectionV2("Memories available:\n%s\n\nRelevant memories 会被临时注入到当前请求中。你必须尊重 memory 中记录的用户偏好、反馈、项目事实和参考信息。当用户说“记住”、表达稳定偏好、给出长期约束或项目事实时，回合结束后应提取为 memory。"),
		StaticSectionV2(
			"agent_loop",
			"在开始任何多步骤任务前，必须先使用 todo_write 规划步骤。遇到复杂子问题、需要上下文隔离或独立调查时，优先使用 task 工具启动子智能体，并只接收其最终结论。执行过程中持续更新 todo_write 的状态：开始做某一步前标记为 in_progress，完成后标记为 completed。",
		),
		StaticSectionV2(
			"compact",
			"当上下文过长、历史重复、工具输出过大，或需要释放上下文空间继续任务时，可以调用 compact 工具。compact 会保留当前目标、关键发现、文件变更、用户约束和剩余工作。",
		),
		StaticSectionV2(
			"permission",
			"你可以使用 Bash 和文件工具完成任务。所有破坏性操作都需要用户批准。",
		),
		StaticSectionV2(
			"style",
			"回答时保持可爱但专业的猫猫娘语气，按状态少量使用 Emoji（如 🐾执行中、✅完成、⚠️注意、❌失败、📌总结）。",
		),
	}
}

// S16SectionsV2 对标 Python S16 PROMPT_SECTIONS 增量。
//
// 在基础 section 列表中插入 team_protocol，解释 request/response 协议工具的使用语义。
func S16SectionsV2() []SectionV2 {
	sections := BaseSectionsV2()

	return InsertSectionAfterV2(
		sections,
		"agent_loop",
		ToolGatedSectionV2(
			"team_protocol",
			"团队协议：request_shutdown 会向 teammate 发送 shutdown_request，并等待 shutdown_response；request_plan 用于要求 teammate 提交计划；teammate 会用 submit_plan 发送 plan_approval_request；Lead 必须用 review_plan 根据 request_id 批准或拒绝计划。check_inbox 会自动路由协议响应。",
			"request_shutdown",
			"request_plan",
			"review_plan",
		),
	)
}

// InsertSectionAfterV2 对标 Python sections.append / 插入 prompt section。
//
// 课程可以在基础列表中的指定 section 后插入自己的新增片段。
func InsertSectionAfterV2(
	sections []SectionV2,
	afterKey string,
	section SectionV2,
) []SectionV2 {
	out := make([]SectionV2, 0, len(sections)+1)
	inserted := false

	for _, existing := range sections {
		out = append(out, existing)

		if !inserted && existing.Key == afterKey {
			out = append(out, section)
			inserted = true
		}
	}

	if !inserted {
		out = append(out, section)
	}

	return out
}

// AssembleV2 对标 Python assemble_system_prompt。
//
// 按调用方传入的 section 顺序渲染 system prompt，空 section 会被跳过。
func AssembleV2(ctx PromptContext, sections []SectionV2) string {
	parts := make([]string, 0, len(sections))

	for _, section := range sections {
		if section.Build == nil {
			continue
		}

		text, ok := section.Build(ctx)
		text = strings.TrimSpace(text)
		if !ok || text == "" {
			continue
		}

		parts = append(parts, text)
	}

	return strings.Join(parts, "\n\n")
}

// Get 对标 Python get_system_prompt。
//
// 使用 context + section keys 作为缓存 key；同一课程 prompt 不变时复用上一份结果。
func (c *CacheV2) Get(
	ctx PromptContext,
	sections []SectionV2,
) string {
	key := contextKey(ctx) + "\nsections:" + strings.Join(sectionCacheKeysV2(sections), "\n")
	if key == c.lastContextKey && c.lastPrompt != "" {
		fmt.Println(" \033[90m[cache hit] system prompt v2 unchanged\033[0m")
		return c.lastPrompt
	}

	c.lastContextKey = key
	c.lastPrompt = AssembleV2(ctx, sections)

	fmt.Printf(
		" \033[32m[assembled v2] sections: %s\033[0m\n",
		strings.Join(enabledSectionKeysV2(ctx, sections), ", "),
	)

	return c.lastPrompt
}

func sectionCacheKeysV2(sections []SectionV2) []string {
	keys := make([]string, 0, len(sections))

	for _, section := range sections {
		keys = append(keys, section.Key+":"+section.Signature)
	}

	return keys
}

func hasAnyEnabledToolV2(enabledTools []string, names []string) bool {
	if len(names) == 0 {
		return true
	}

	for _, enabled := range enabledTools {
		for _, name := range names {
			if enabled == name {
				return true
			}
		}
	}

	return false
}

func enabledSectionKeysV2(
	ctx PromptContext,
	sections []SectionV2,
) []string {
	keys := make([]string, 0, len(sections))

	for _, section := range sections {
		if section.Build == nil {
			continue
		}

		text, ok := section.Build(ctx)
		if ok && strings.TrimSpace(text) != "" {
			keys = append(keys, section.Key)
		}
	}

	return keys
}
