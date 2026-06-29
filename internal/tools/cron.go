package tools

import (
	cronstore "AgentLoop/internal/cron"
	v2 "AgentLoop/internal/toolkit/v2"
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// ScheduleCronArgs 对标 Python run_schedule_cron 的参数。
//
// recurring 和 durable 使用指针是为了区分“模型未传”和“模型显式传 false”。
type ScheduleCronArgs struct {
	Cron      string `json:"cron"`
	Prompt    string `json:"prompt"`
	Recurring *bool  `json:"recurring,omitempty"`
	Durable   *bool  `json:"durable,omitempty"`
}

// CancelCronArgs 对标 Python run_cancel_cron 的参数。
//
// 表示需要取消的 cron job ID。
type CancelCronArgs struct {
	JobID string `json:"job_id"`
}

// executeScheduleCron 对标 Python run_schedule_cron。
//
// 解析工具参数，补齐 recurring/durable 默认值，并注册 cron 任务。
func executeScheduleCron(
	scheduler *cronstore.Scheduler,
) func(context.Context, json.RawMessage) (string, error) {
	return func(
		_ context.Context,
		arguments json.RawMessage,
	) (string, error) {
		if scheduler == nil {
			return "", fmt.Errorf("cron scheduler is nil")
		}

		var args ScheduleCronArgs
		if err := json.Unmarshal(arguments, &args); err != nil {
			return "", err
		}

		args.Cron = strings.TrimSpace(args.Cron)
		args.Prompt = strings.TrimSpace(args.Prompt)

		if args.Cron == "" {
			return "", fmt.Errorf("cron is required")
		}

		if args.Prompt == "" {
			return "", fmt.Errorf("prompt is required")
		}

		recurring := true
		if args.Recurring != nil {
			recurring = *args.Recurring
		}

		durable := true
		if args.Durable != nil {
			durable = *args.Durable
		}

		job, err := scheduler.Schedule(args.Cron, args.Prompt, recurring, durable)
		if err != nil {
			return "Error: " + err.Error(), nil
		}

		return fmt.Sprintf("Scheduled %s: %q -> %s", job.ID, job.Cron, job.Prompt), nil
	}
}

// executeListCrons 对标 Python run_list_crons。
//
// 读取当前全部 cron 任务，并渲染 recurring/session 等状态。
func executeListCrons(
	scheduler *cronstore.Scheduler,
) func(context.Context, json.RawMessage) (string, error) {
	return func(
		_ context.Context,
		_ json.RawMessage,
	) (string, error) {
		if scheduler == nil {
			return "", fmt.Errorf("cron scheduler is nil")
		}

		jobs := scheduler.List()
		if len(jobs) == 0 {
			return "No cron jobs. Use schedule_cron to add one.", nil
		}

		var b strings.Builder

		for _, job := range jobs {
			kind := "recurring"
			if !job.Recurring {
				kind = "one-shot"
			}

			durability := "durable"
			if !job.Durable {
				durability = "session"
			}

			prompt := strings.ReplaceAll(job.Prompt, "\n", " ")

			fmt.Fprintf(
				&b,
				"  %s: %q -> %s [%s, %s]\n",
				job.ID,
				job.Cron,
				previewText(prompt, 40),
				kind,
				durability,
			)
		}

		return strings.TrimRight(b.String(), "\n"), nil
	}
}

// executeCancelCron 对标 Python run_cancel_cron。
//
// 根据 job_id 删除一个已经注册的 cron 任务。
func executeCancelCron(
	scheduler *cronstore.Scheduler,
) func(context.Context, json.RawMessage) (string, error) {
	return func(
		_ context.Context,
		arguments json.RawMessage,
	) (string, error) {
		if scheduler == nil {
			return "", fmt.Errorf("cron scheduler is nil")
		}

		var args CancelCronArgs
		if err := json.Unmarshal(arguments, &args); err != nil {
			return "", err
		}

		args.JobID = strings.TrimSpace(args.JobID)
		if args.JobID == "" {
			return "", fmt.Errorf("job_id is required")
		}

		job, ok, err := scheduler.Cancel(args.JobID)
		if err != nil {
			return "", err
		}

		if !ok {
			return fmt.Sprintf("Job %s not found", args.JobID), nil
		}

		return fmt.Sprintf("Cancelled %s", job.ID), nil
	}
}

// NewScheduleCronToolV2 对标 Python schedule_cron tool schema。
//
// 注册一个可让模型创建 cron 计划任务的工具。
func NewScheduleCronToolV2(scheduler *cronstore.Scheduler) v2.Tool {
	return v2.NewFunctionTool(
		"schedule_cron",
		"Schedule a cron job. cron is 5-field: min hour dom month dow.",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"cron": map[string]any{
					"type":        "string",
					"description": "5-field cron expression: min hour dom month dow.",
				},
				"prompt": map[string]any{
					"type":        "string",
					"description": "Message to inject when the cron job fires.",
				},
				"recurring": map[string]any{
					"type":        "boolean",
					"description": "True for recurring jobs, false for one-shot jobs. Defaults to true.",
				},
				"durable": map[string]any{
					"type":        "boolean",
					"description": "True to persist the job to .scheduled_tasks.json. Defaults to true.",
				},
			},
			"required":             []string{"cron", "prompt"},
			"additionalProperties": false,
		},
		executeScheduleCron(scheduler),
	)
}

// NewListCronsToolV2 对标 Python list_crons tool schema。
//
// 注册列出全部 cron 计划任务的工具。
func NewListCronsToolV2(scheduler *cronstore.Scheduler) v2.Tool {
	return v2.NewFunctionTool(
		"list_crons",
		"List all registered cron jobs.",
		map[string]any{
			"type":                 "object",
			"properties":           map[string]any{},
			"required":             []string{},
			"additionalProperties": false,
		},
		executeListCrons(scheduler),
	)
}

// NewCancelCronToolV2 对标 Python cancel_cron tool schema。
//
// 注册取消指定 cron 计划任务的工具。
func NewCancelCronToolV2(scheduler *cronstore.Scheduler) v2.Tool {
	return v2.NewFunctionTool(
		"cancel_cron",
		"Cancel a cron job by ID.",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"job_id": map[string]any{
					"type":        "string",
					"description": "ID of the cron job to cancel.",
				},
			},
			"required":             []string{"job_id"},
			"additionalProperties": false,
		},
		executeCancelCron(scheduler),
	)
}

func previewText(text string, limit int) string {
	if limit <= 0 {
		return ""
	}

	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}

	return string(runes[:limit])
}
