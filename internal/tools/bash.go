package tools

import (
	v2 "AgentLoop/internal/toolkit/v2"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

const (
	commandLimit   = 50_000
	commandTimeout = 120 * time.Second
)

type BashArgs struct {
	Command string `json:"command"`
}

func runBash(ctx context.Context, command string) string {
	dangerous := []string{
		"rm -rf /",
		"sudo",
		"shutdown",
		"reboot",
		"> /dev/",
	}
	for _, d := range dangerous {
		if strings.Contains(command, d) {
			return "Error:Dangerous command blocked"
		}
	}
	ctx, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()

	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(ctx, "cmd", "/C", command)
	} else {
		cmd = exec.CommandContext(ctx, "bash", "-lc", command)
	}

	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Sprintf("Error:%v", err)
	}
	cmd.Dir = cwd
	out, err := cmd.CombinedOutput()

	if ctx.Err() == context.DeadlineExceeded {
		return fmt.Sprintf("Error: Timeout(%.2f)", commandTimeout.Seconds())
	}

	text := strings.TrimSpace(string(out))

	if err != nil {
		if text == "" {
			return fmt.Sprintf("Error: %v", err)
		}
		text = text + "\n" + err.Error()
	}

	if text == "" {
		return "OK: command completed with no output"
	}

	runes := []rune(text)
	if len(runes) > commandLimit {
		return string(runes[:commandLimit]) + "\n...output truncated"
	}

	return text
}

func executeRunBash(ctx context.Context, arguments json.RawMessage) (string, error) {
	var args BashArgs
	if err := json.Unmarshal(arguments, &args); err != nil {
		return "", err
	}
	if strings.TrimSpace(args.Command) == "" {
		return "", fmt.Errorf("command is required")
	}
	return runBash(ctx, args.Command), nil
}
func NewBashToolV2() v2.Tool {
	return v2.NewFunctionTool(
		"bash",
		"Run a shell command.",
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
		executeRunBash,
	)
}
