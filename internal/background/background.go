package background

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
)

type Status string

const (
	StatusRunning   Status = "running"
	StatusCompleted Status = "completed"
)

// Task 对标 Python background_tasks / background_results 中的一条记录。
//
// 它只保存一个进程内后台工具调用的状态与最终输出。
type Task struct {
	ID         string
	ToolCallID string
	ToolName   string
	Command    string
	Status     Status
	Output     string
}

// Tracker 对标 Python background_tasks、background_results 和 background_lock。
//
// 它是一个轻量状态容器，只维护当前进程中的后台任务。
type Tracker struct {
	mu      sync.Mutex
	counter int
	tasks   map[string]Task
}

type bashArgs struct {
	Command         string `json:"command"`
	RunInBackground bool   `json:"run_in_background"`
}

// NewTracker 对标 Python 后台任务全局字典初始化。
//
// 创建当前 Agent 进程使用的内存后台任务追踪器。
func NewTracker() *Tracker {
	return &Tracker{
		tasks: make(map[string]Task),
	}
}

// IsSlowOperation 对标 Python is_slow_operation。
//
// 当模型没有显式指定 run_in_background 时，通过命令关键词做兜底判断。
func IsSlowOperation(
	toolName string,
	arguments json.RawMessage,
) bool {
	if toolName != "bash" {
		return false
	}

	var args bashArgs
	if err := json.Unmarshal(arguments, &args); err != nil {
		return false
	}

	slowKeywords := []string{
		"install",
		"build",
		"test",
		"deploy",
		"compile",
		"docker build",
		"pip install",
		"npm install",
		"cargo build",
		"pytest",
		"make",
	}

	command := strings.ToLower(args.Command)
	for _, keyword := range slowKeywords {
		if strings.Contains(command, keyword) {
			return true
		}
	}

	return false
}

// ShouldRun 对标 Python should_run_background。
//
// run_in_background=true 优先；没有显式请求时再使用慢命令关键词判断。
func ShouldRun(
	toolName string,
	arguments json.RawMessage,
) bool {
	if toolName != "bash" {
		return false
	}

	var args bashArgs
	if err := json.Unmarshal(arguments, &args); err != nil {
		return false
	}

	if args.RunInBackground {
		return true
	}

	return IsSlowOperation(toolName, arguments)
}

// Start 对标 Python start_background_task。
//
// 立即注册 running 状态并启动 goroutine；调用方会同步返回占位 tool result。
func (t *Tracker) Start(
	toolCallID string,
	toolName string,
	arguments json.RawMessage,
	run func() string,
) Task {
	t.mu.Lock()

	t.counter++

	task := Task{
		ID:         fmt.Sprintf("bg_%04d", t.counter),
		ToolCallID: toolCallID,
		ToolName:   toolName,
		Command:    commandLabel(toolName, arguments),
		Status:     StatusRunning,
	}

	t.tasks[task.ID] = task

	t.mu.Unlock()

	fmt.Printf(
		"  \033[33m[background] dispatched %s: %s\033[0m\n",
		task.ID,
		previewRunes(task.Command, 40),
	)

	go func(taskID string) {
		output := run()

		t.mu.Lock()
		defer t.mu.Unlock()

		current, ok := t.tasks[taskID]
		if !ok {
			return
		}

		current.Status = StatusCompleted
		current.Output = output
		t.tasks[taskID] = current
	}(task.ID)

	return task
}

// Collect 对标 Python collect_background_results。
//
// 取出已经完成的任务，并生成不带 tool_call_id 的独立 task_notification。
func (t *Tracker) Collect() []string {
	t.mu.Lock()

	readyIDs := make([]string, 0)

	for id, task := range t.tasks {
		if task.Status == StatusCompleted {
			readyIDs = append(readyIDs, id)
		}
	}

	sort.Strings(readyIDs)

	ready := make([]Task, 0, len(readyIDs))
	for _, id := range readyIDs {
		ready = append(ready, t.tasks[id])
		delete(t.tasks, id)
	}

	t.mu.Unlock()

	notifications := make([]string, 0, len(ready))

	for _, task := range ready {
		summary := previewRunes(task.Output, 200)

		notifications = append(
			notifications,
			fmt.Sprintf(
				"<task_notification>\n"+
					"  <task_id>%s</task_id>\n"+
					"  <status>completed</status>\n"+
					"  <command>%s</command>\n"+
					"  <summary>%s</summary>\n"+
					"</task_notification>",
				task.ID,
				task.Command,
				summary,
			),
		)

		fmt.Printf(
			"  \033[32m[background done] %s: %s (%d chars)\033[0m\n",
			task.ID,
			previewRunes(task.Command, 40),
			len([]rune(task.Output)),
		)
	}

	return notifications
}
func commandLabel(
	toolName string,
	arguments json.RawMessage,
) string {
	if toolName != "bash" {
		return toolName
	}

	var args bashArgs
	if err := json.Unmarshal(arguments, &args); err != nil {
		return toolName
	}

	command := strings.TrimSpace(args.Command)
	if command == "" {
		return toolName
	}

	return command
}

func previewRunes(text string, limit int) string {
	if limit <= 0 {
		return ""
	}

	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}

	return string(runes[:limit])
}
