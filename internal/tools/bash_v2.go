package tools

import (
	v2 "AgentLoop/internal/toolkit/v2"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

const bashTimeout = 120 * time.Second

type BashArgsV2 struct {
	Command string `json:"command"`

	// RunInBackground 对标 Python bash schema 的 run_in_background。
	//
	// 该字段只由 S13 主循环读取，executeBash 本身仍然同步执行命令。
	RunInBackground bool `json:"run_in_background,omitempty"`
}

// executeBash 对标 Python run_bash。
//
// 执行实际 shell 命令；是否放入后台由 S13 agent loop 决定。
func executeBash() func(context.Context, json.RawMessage) (string, error) {
	return func(
		ctx context.Context,
		arguments json.RawMessage,
	) (string, error) {
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

		raw, err := cmd.CombinedOutput()

		if commandContext.Err() == context.DeadlineExceeded {
			return "Error: Timeout (120s)", nil
		}

		output := strings.TrimSpace(string(raw))

		// 对标 Python：命令返回非零状态时，仍把 stdout/stderr 交给模型。
		if output == "" && err != nil {
			return fmt.Sprintf("Error: %v", err), nil
		}

		if output == "" {
			return "(no output)", nil
		}

		runes := []rune(output)
		if len(runes) > 50000 {
			output = string(runes[:50000])
		}

		return output, nil
	}
}

// NewBashV2ToolV2 对标 S02-S12 的 bash schema。
//
// 前序章节不暴露 run_in_background，保持课程递进。
func NewBashV2ToolV2() v2.Tool {
	return newBashToolV2(false)
}

// NewBashToolV2WithBackground 对标 S13 bash schema。
//
// 增加可选 run_in_background 参数，让模型显式请求后台执行。
func NewBashV2ToolV2WithBackground() v2.Tool {
	return newBashToolV2(true)
}

func newBashToolV2(enableBackground bool) v2.Tool {
	properties := map[string]any{
		"command": map[string]any{
			"type":        "string",
			"description": "Shell command to execute.",
		},
	}

	description := "Run a shell command."

	if enableBackground {
		properties["run_in_background"] = map[string]any{
			"type":        "boolean",
			"description": "Set true for slow commands so the agent can continue other work while the command runs.",
		}

		description = "Run a shell command. Slow commands may run in the background."
	}

	return v2.NewFunctionTool(
		"bash",
		description,
		map[string]any{
			"type":                 "object",
			"properties":           properties,
			"required":             []string{"command"},
			"additionalProperties": false,
		},
		executeBash(),
	)
}
