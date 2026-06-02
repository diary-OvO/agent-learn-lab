package tools

import (
	v2 "AgentLoop/internal/toolkit/v2"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	maxOutputLen = 50_000
)

// WORKDIR 表示 Agent 允许操作的工作区根目录。
// 初始化时转成绝对路径，后续 safePath 就不用反复 Abs。
var WORKDIR = mustWorkdir()

type ReadFileArgs struct {
	Path  string `json:"path"`
	Limit int    `json:"limit,omitempty"`
}

type WriteFileArgs struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

type EditFileArgs struct {
	Path    string `json:"path"`
	OldText string `json:"old_text"`
	NewText string `json:"new_text"`
}

type GlobArgs struct {
	Pattern string `json:"pattern"`
}

// runRead 读取工作区内的文件。
// limit > 0 时，只返回前 limit 行。
func runRead(path string, limit int) string {
	fp, err := SafePath(path)
	if err != nil {
		return fmt.Sprintf("Error:%v", err)
	}

	data, err := os.ReadFile(fp)
	if err != nil {
		return fmt.Sprintf("Error:%v", err)
	}

	text := string(data)
	lines := strings.Split(text, "\n")

	if limit > 0 && limit < len(lines) {
		more := len(lines) - limit
		lines = append(lines[:limit], fmt.Sprintf("... (%d more lines)", more))
	}

	result := strings.Join(lines, "\n")

	runes := []rune(result)
	if len(runes) > commandLimit {
		return string(runes[:commandLimit]) + "\n...output truncated"
	}

	return result
}

func executeReadFile(ctx context.Context, arguments json.RawMessage) (string, error) {
	var args ReadFileArgs
	if err := json.Unmarshal(arguments, &args); err != nil {
		return "", err
	}

	if strings.TrimSpace(args.Path) == "" {
		return "", fmt.Errorf("path is required")
	}

	return runRead(args.Path, args.Limit), nil
}

func NewReadFileToolV2() v2.Tool {
	return v2.NewFunctionTool(
		"read_file",
		"Read a file from the workspace. Optionally limit the number of returned lines.",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{
					"type":        "string",
					"description": "Relative path of the file to read.",
				},
				"limit": map[string]any{
					"type":        "integer",
					"description": "Optional maximum number of lines to return.",
				},
			},
			"required":             []string{"path"},
			"additionalProperties": false,
		},
		executeReadFile,
	)
}

// runWrite 写入工作区内的文件。
// 如果父目录不存在，会自动创建。
func runWrite(path string, content string) string {
	fp, err := SafePath(path)
	if err != nil {
		return fmt.Sprintf("Error:%v", err)
	}

	if err := os.MkdirAll(filepath.Dir(fp), 0o755); err != nil {
		return fmt.Sprintf("Error:%v", err)
	}

	if err := os.WriteFile(fp, []byte(content), 0o644); err != nil {
		return fmt.Sprintf("Error:%v", err)
	}

	return fmt.Sprintf("Wrote %d bytes to %s", len([]byte(content)), path)
}

func executeWriteFile(ctx context.Context, arguments json.RawMessage) (string, error) {
	var args WriteFileArgs
	if err := json.Unmarshal(arguments, &args); err != nil {
		return "", err
	}

	if strings.TrimSpace(args.Path) == "" {
		return "", fmt.Errorf("path is required")
	}

	return runWrite(args.Path, args.Content), nil
}

func NewWriteFileToolV2() v2.Tool {
	return v2.NewFunctionTool(
		"write_file",
		"Write content to a file in the workspace. Creates parent directories if needed.",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{
					"type":        "string",
					"description": "Relative path of the file to write.",
				},
				"content": map[string]any{
					"type":        "string",
					"description": "Content to write into the file.",
				},
			},
			"required":             []string{"path", "content"},
			"additionalProperties": false,
		},
		executeWriteFile,
	)
}

// runEdit 编辑工作区内的文件。
// 只替换第一次出现的 oldText，避免误伤多个相同片段。
func runEdit(path string, oldText string, newText string) string {
	fp, err := SafePath(path)
	if err != nil {
		return fmt.Sprintf("Error:%v", err)
	}

	data, err := os.ReadFile(fp)
	if err != nil {
		return fmt.Sprintf("Error:%v", err)
	}

	content := string(data)

	if !strings.Contains(content, oldText) {
		return fmt.Sprintf("Error: Text not found in %s", path)
	}

	updated := strings.Replace(content, oldText, newText, 1)

	if err := os.WriteFile(fp, []byte(updated), 0o644); err != nil {
		return fmt.Sprintf("Error:%v", err)
	}

	return fmt.Sprintf("Edited %s", path)
}

func executeEditFile(ctx context.Context, arguments json.RawMessage) (string, error) {
	var args EditFileArgs
	if err := json.Unmarshal(arguments, &args); err != nil {
		return "", err
	}

	if strings.TrimSpace(args.Path) == "" {
		return "", fmt.Errorf("path is required")
	}

	if args.OldText == "" {
		return "", fmt.Errorf("old_text is required")
	}

	return runEdit(args.Path, args.OldText, args.NewText), nil
}

func NewEditFileToolV2() v2.Tool {
	return v2.NewFunctionTool(
		"edit_file",
		"Edit a file by replacing the first occurrence of old_text with new_text.",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{
					"type":        "string",
					"description": "Relative path of the file to edit.",
				},
				"old_text": map[string]any{
					"type":        "string",
					"description": "Exact text to replace. Must exist in the file.",
				},
				"new_text": map[string]any{
					"type":        "string",
					"description": "Replacement text.",
				},
			},
			"required":             []string{"path", "old_text", "new_text"},
			"additionalProperties": false,
		},
		executeEditFile,
	)
}

// runGlob 查找工作区内匹配 glob pattern 的文件或目录。
// pattern 必须是相对 WORKDIR 的路径，例如：
//
//	"*.go"
//	"test/*.go"
//	"internal/*/*.go"
func runGlob(pattern string) string {
	absPattern, err := safeGlobPattern(pattern)
	if err != nil {
		return fmt.Sprintf("Error:%v", err)
	}

	matches, err := filepath.Glob(absPattern)
	if err != nil {
		return fmt.Sprintf("Error:%v", err)
	}

	if len(matches) == 0 {
		return "(no matches)"
	}

	results := make([]string, 0, len(matches))

	for _, match := range matches {
		// 先确认匹配路径本身在 WORKDIR 内。
		relMatch, err := filepath.Rel(WORKDIR, match)
		if err != nil || !filepath.IsLocal(relMatch) {
			continue
		}

		// 如果 match 是软链接，尽量解析真实路径。
		// 如果真实路径逃出 WORKDIR，则跳过。
		realMatch := match
		if real, err := filepath.EvalSymlinks(match); err == nil {
			realMatch = real
		}

		relReal, err := filepath.Rel(WORKDIR, realMatch)
		if err != nil || !filepath.IsLocal(relReal) {
			continue
		}

		// 返回给模型时使用相对路径，并统一成 slash，避免 Windows 反斜杠影响模型理解。
		results = append(results, filepath.ToSlash(relMatch))
	}

	if len(results) == 0 {
		return "(no matches)"
	}

	output := strings.Join(results, "\n")

	runes := []rune(output)
	if len(runes) > maxOutputLen {
		return string(runes[:maxOutputLen]) + "\n...output truncated"
	}

	return output
}

func executeGlob(ctx context.Context, arguments json.RawMessage) (string, error) {
	var args GlobArgs
	if err := json.Unmarshal(arguments, &args); err != nil {
		return "", err
	}

	if strings.TrimSpace(args.Pattern) == "" {
		return "", fmt.Errorf("pattern is required")
	}

	return runGlob(args.Pattern), nil
}

func NewGlobToolV2() v2.Tool {
	return v2.NewFunctionTool(
		"glob",
		"Find files matching a glob pattern in the workspace.",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"pattern": map[string]any{
					"type":        "string",
					"description": "Glob pattern relative to the workspace, for example: *.go, test/*.go, internal/*/*.go.",
				},
			},
			"required":             []string{"pattern"},
			"additionalProperties": false,
		},
		executeGlob,
	)
}

// safeGlobPattern 校验 glob pattern 是否仍然限制在 WORKDIR 内。
// 不能直接用 SafePath，因为 SafePath 面向真实文件路径，
// 而 glob pattern 里可能包含 *, ?, [] 等通配符。
func safeGlobPattern(pattern string) (string, error) {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return "", fmt.Errorf("pattern is required")
	}

	// 统一处理模型经常生成的 Unix 风格路径。
	pattern = filepath.FromSlash(pattern)

	// 清理路径，例如 ./a/*.go -> a/*.go。
	pattern = filepath.Clean(pattern)

	// 禁止绝对路径、空路径、../ 逃逸路径。
	if !filepath.IsLocal(pattern) {
		return "", fmt.Errorf("pattern escapes workspace: %s", pattern)
	}

	return filepath.Join(WORKDIR, pattern), nil
}

func mustWorkdir() string {
	wd, err := os.Getwd()
	if err != nil {
		panic(err)
	}

	abs, err := filepath.Abs(wd)
	if err != nil {
		panic(err)
	}

	// 如果工作区本身是软链接，尽量解析到真实路径。
	if real, err := filepath.EvalSymlinks(abs); err == nil {
		return real
	}

	return abs
}

// SafePath 将用户传入的相对路径限制在 WORKDIR 内。
// 作用等价于 Python:
//
//	path = (WORKDIR / p).resolve()
//	if not path.is_relative_to(WORKDIR):
//	    raise ValueError(...)
func SafePath(p string) (string, error) {
	// IsLocal 会拒绝空路径、绝对路径、以及 ../ 逃逸路径。
	if !filepath.IsLocal(p) {
		return "", fmt.Errorf("path escapes workspace: %s", p)
	}

	// Join 会自动清理路径，例如 a/../b -> b。
	path := filepath.Join(WORKDIR, p)

	// 如果目标已存在，解析软链接，防止 read/edit 通过软链接跳出 WORKDIR。
	if real, err := filepath.EvalSymlinks(path); err == nil {
		path = real
	}

	// 再确认最终路径仍然在 WORKDIR 内。
	rel, err := filepath.Rel(WORKDIR, path)
	if err != nil || !filepath.IsLocal(rel) {
		return "", fmt.Errorf("path escapes workspace: %s", p)
	}

	return path, nil
}
