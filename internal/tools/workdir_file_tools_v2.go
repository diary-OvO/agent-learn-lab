package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	v2 "AgentLoop/internal/toolkit/v2"
)

// CWDProvider 对标 Python S18 wt_ctx["path"]。
//
// teammate 工具执行前通过它读取当前工作目录；空值表示仍使用主工作区。
type CWDProvider func() string

// executeBashWithCWD 对标 Python S18 _run_bash(command)。
//
// 迭代原因：S18 teammate 认领绑定 worktree 的任务后，bash 必须在该 worktree 目录运行。
//
// 与 executeBash 差别：旧函数始终在进程当前目录执行；这里每次调用前读取 cwdProvider，
// 让同一个 teammate 在不同任务之间切换 cwd。
func executeBashWithCWD(
	cwdProvider CWDProvider,
) func(context.Context, json.RawMessage) (string, error) {
	return func(ctx context.Context, arguments json.RawMessage) (string, error) {
		var args BashArgsV2
		if err := json.Unmarshal(arguments, &args); err != nil {
			return "", err
		}

		args.Command = strings.TrimSpace(args.Command)
		if args.Command == "" {
			return "", fmt.Errorf("command is required")
		}

		commandContext, cancel := context.WithTimeout(ctx, bashTimeout)
		defer cancel()

		cmd := exec.CommandContext(
			commandContext,
			"bash",
			"-lc",
			args.Command,
		)
		cmd.Dir = cwdOrWorkdir(cwdProvider)

		raw, err := cmd.CombinedOutput()

		if commandContext.Err() == context.DeadlineExceeded {
			return "Error: Timeout (120s)", nil
		}

		output := strings.TrimSpace(string(raw))
		if output == "" && err != nil {
			return fmt.Sprintf("Error: %v", err), nil
		}
		if output == "" {
			return "(no output)", nil
		}

		return previewRunes(output, maxOutputLen), nil
	}
}

// NewBashV2ToolV2WithCWD 对标 Python S18 bash teammate tool schema。
//
// 注册 worktree-aware bash；旧 NewBashV2ToolV2 继续服务 S17 之前的 teammate。
func NewBashV2ToolV2WithCWD(cwdProvider CWDProvider) v2.Tool {
	return v2.NewFunctionTool(
		"bash",
		"Run a shell command in the teammate current work directory.",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"command": map[string]any{
					"type":        "string",
					"description": "Shell command to execute.",
				},
			},
			"required":             []string{"command"},
			"additionalProperties": false,
		},
		executeBashWithCWD(cwdProvider),
	)
}

// executeReadFileWithCWD 对标 Python S18 _run_read(path)。
//
// 迭代原因：S18 teammate 在处理 worktree task 时，read_file 应限制在当前 worktree cwd。
//
// 与 executeReadFile 差别：旧函数使用全局 WORKDIR；这里使用 cwdProvider，并对路径做
// base-relative 校验，防止从 worktree 逃回主工作区或其他目录。
func executeReadFileWithCWD(
	cwdProvider CWDProvider,
) func(context.Context, json.RawMessage) (string, error) {
	return func(_ context.Context, arguments json.RawMessage) (string, error) {
		var args ReadFileArgs
		if err := json.Unmarshal(arguments, &args); err != nil {
			return "", err
		}

		if strings.TrimSpace(args.Path) == "" {
			return "", fmt.Errorf("path is required")
		}

		path, err := safePathInBase(cwdOrWorkdir(cwdProvider), args.Path)
		if err != nil {
			return "", err
		}

		raw, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}

		lines := strings.Split(string(raw), "\n")
		if args.Limit > 0 && args.Limit < len(lines) {
			lines = append(
				lines[:args.Limit],
				fmt.Sprintf("... (%d more lines)", len(lines)-args.Limit),
			)
		}

		return previewRunes(strings.Join(lines, "\n"), maxOutputLen), nil
	}
}

// NewReadFileToolV2WithCWD 对标 Python S18 read_file teammate tool schema。
//
// 注册 worktree-aware read_file；只有 S18 teammate toolbox 使用。
func NewReadFileToolV2WithCWD(cwdProvider CWDProvider) v2.Tool {
	return v2.NewFunctionTool(
		"read_file",
		"Read a file from the teammate current work directory.",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{
					"type":        "string",
					"description": "Relative file path.",
				},
				"limit": map[string]any{
					"type":        "integer",
					"description": "Optional maximum number of lines to return.",
				},
			},
			"required":             []string{"path"},
			"additionalProperties": false,
		},
		executeReadFileWithCWD(cwdProvider),
	)
}

// executeWriteFileWithCWD 对标 Python S18 _run_write(path, content)。
//
// 迭代原因：S18 teammate 对绑定 worktree 的任务写文件时，应写入隔离目录而不是主仓库。
//
// 与 executeWriteFile 差别：旧函数固定 SafePath(WORKDIR)；这里使用当前 cwd，并保持
// 相同的父目录自动创建行为。
func executeWriteFileWithCWD(
	cwdProvider CWDProvider,
) func(context.Context, json.RawMessage) (string, error) {
	return func(_ context.Context, arguments json.RawMessage) (string, error) {
		var args WriteFileArgs
		if err := json.Unmarshal(arguments, &args); err != nil {
			return "", err
		}

		if strings.TrimSpace(args.Path) == "" {
			return "", fmt.Errorf("path is required")
		}

		base := cwdOrWorkdir(cwdProvider)
		path, err := safePathInBase(base, args.Path)
		if err != nil {
			return "", err
		}

		if err := ensureParentInBase(base, path); err != nil {
			return "", err
		}

		if err := os.WriteFile(path, []byte(args.Content), 0o644); err != nil {
			return "", err
		}

		return fmt.Sprintf("Wrote %d bytes to %s", len([]byte(args.Content)), args.Path), nil
	}
}

// NewWriteFileToolV2WithCWD 对标 Python S18 write_file teammate tool schema。
//
// 注册 worktree-aware write_file；旧 NewWriteFileToolV2 不变，供 Lead 和前序课程使用。
func NewWriteFileToolV2WithCWD(cwdProvider CWDProvider) v2.Tool {
	return v2.NewFunctionTool(
		"write_file",
		"Write a file in the teammate current work directory.",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{
					"type":        "string",
					"description": "Relative file path.",
				},
				"content": map[string]any{
					"type":        "string",
					"description": "Content to write into the file.",
				},
			},
			"required":             []string{"path", "content"},
			"additionalProperties": false,
		},
		executeWriteFileWithCWD(cwdProvider),
	)
}

func cwdOrWorkdir(cwdProvider CWDProvider) string {
	base := ""
	if cwdProvider != nil {
		base = strings.TrimSpace(cwdProvider())
	}
	if base == "" {
		base = WORKDIR
	}

	return normalizeBasePath(base)
}

func normalizeBasePath(base string) string {
	abs, err := filepath.Abs(base)
	if err != nil {
		return WORKDIR
	}

	if real, err := filepath.EvalSymlinks(abs); err == nil {
		abs = real
	}

	return abs
}

func safePathInBase(base string, path string) (string, error) {
	base = normalizeBasePath(base)
	path = filepath.FromSlash(strings.TrimSpace(path))

	info, err := os.Stat(base)
	if err != nil || !info.IsDir() {
		return "", fmt.Errorf("work directory unavailable: %s", base)
	}

	if !filepath.IsLocal(path) {
		return "", fmt.Errorf("path escapes workspace: %s", path)
	}

	target := filepath.Join(base, path)
	if real, err := filepath.EvalSymlinks(target); err == nil {
		target = real
	}

	rel, err := filepath.Rel(base, target)
	if err != nil || !filepath.IsLocal(rel) {
		return "", fmt.Errorf("path escapes workspace: %s", path)
	}

	return target, nil
}

func ensureParentInBase(base string, path string) error {
	base = normalizeBasePath(base)
	parent := filepath.Dir(path)

	if err := os.MkdirAll(parent, 0o755); err != nil {
		return err
	}

	realParent := parent
	if real, err := filepath.EvalSymlinks(parent); err == nil {
		realParent = real
	}

	rel, err := filepath.Rel(base, realParent)
	if err != nil || !filepath.IsLocal(rel) {
		return fmt.Errorf("path escapes workspace: %s", path)
	}

	return nil
}
