package tasks

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	StatusPending    = "pending"
	StatusInProgress = "in_progress"
	StatusCompleted  = "completed"
)

// Task 对标 Python Task dataclass。
//
// 表示一个可跨会话持久化、可被认领并带 blockedBy 依赖的任务。
type Task struct {
	ID          string   `json:"id"`
	Subject     string   `json:"subject"`
	Description string   `json:"description"`
	Status      string   `json:"status"`
	Owner       *string  `json:"owner"`
	BlockedBy   []string `json:"blockedBy"`
}

// Store 对标 Python TASKS_DIR。
//
// 它只保存 .tasks 目录这一稳定状态，不持有模型、工具箱或 Agent Loop。
type Store struct {
	Dir string
}

// NewStore 对标 Python TASKS_DIR.mkdir(exist_ok=True)。
//
// 创建并返回当前工作区的任务存储目录。
func NewStore(workDir string) (Store, error) {
	dir := filepath.Join(workDir, ".tasks")

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return Store{}, err
	}

	return Store{
		Dir: dir,
	}, nil
}

// Create 对标 Python create_task。
//
// 创建一个 pending 任务，生成简单任务 ID，并立即写入磁盘。
func (s Store) Create(
	subject string,
	description string,
	blockedBy []string,
) (Task, error) {
	task := Task{
		ID: fmt.Sprintf(
			"task_%d_%04d",
			time.Now().Unix(),
			rand.Intn(10000),
		),
		Subject:     subject,
		Description: description,
		Status:      StatusPending,
		Owner:       nil,
		BlockedBy:   append([]string(nil), blockedBy...),
	}

	if task.BlockedBy == nil {
		task.BlockedBy = []string{}
	}

	if err := s.Save(task); err != nil {
		return Task{}, err
	}

	return task, nil
}

// Save 对标 Python save_task。
//
// 将完整 Task 以缩进 JSON 写入 .tasks/{id}.json。
func (s Store) Save(task Task) error {
	path, err := s.taskPath(task.ID)
	if err != nil {
		return err
	}

	if task.BlockedBy == nil {
		task.BlockedBy = []string{}
	}

	raw, err := json.MarshalIndent(task, "", "  ")
	if err != nil {
		return err
	}

	raw = append(raw, '\n')

	return os.WriteFile(path, raw, 0o600)
}

// Load 对标 Python load_task。
//
// 从任务 ID 对应的 JSON 文件读取完整 Task。
func (s Store) Load(taskID string) (Task, error) {
	path, err := s.taskPath(taskID)
	if err != nil {
		return Task{}, err
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		return Task{}, err
	}

	var task Task
	if err := json.Unmarshal(raw, &task); err != nil {
		return Task{}, err
	}

	if task.BlockedBy == nil {
		task.BlockedBy = []string{}
	}

	return task, nil
}

// List 对标 Python list_tasks。
//
// 按文件名顺序读取 .tasks/task_*.json 中的所有任务。
func (s Store) List() ([]Task, error) {
	entries, err := os.ReadDir(s.Dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	result := make([]Task, 0)

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		filename := entry.Name()
		if !strings.HasPrefix(filename, "task_") ||
			filepath.Ext(filename) != ".json" {
			continue
		}

		taskID := strings.TrimSuffix(filename, ".json")

		task, err := s.Load(taskID)
		if err != nil {
			return nil, err
		}

		result = append(result, task)
	}

	return result, nil
}

// Get 对标 Python get_task。
//
// 返回单个任务的完整缩进 JSON，供 Agent 跨会话恢复任务细节。
func (s Store) Get(taskID string) (string, error) {
	task, err := s.Load(taskID)
	if err != nil {
		return "", err
	}

	raw, err := json.MarshalIndent(task, "", "  ")
	if err != nil {
		return "", err
	}

	return string(raw), nil
}

// CanStart 对标 Python can_start。
//
// 只有 blockedBy 中所有依赖都存在且状态为 completed，任务才可以开始。
func (s Store) CanStart(taskID string) (bool, error) {
	task, err := s.Load(taskID)
	if err != nil {
		return false, err
	}

	for _, dependencyID := range task.BlockedBy {
		dependency, err := s.Load(dependencyID)

		if os.IsNotExist(err) {
			// 对标原课：不存在的依赖也视为 blocked。
			return false, nil
		}
		if err != nil {
			return false, err
		}

		if dependency.Status != StatusCompleted {
			return false, nil
		}
	}

	return true, nil
}

// Claim 对标 Python claim_task。
//
// 检查任务仍为 pending 且依赖均已完成，然后设置 owner 并进入 in_progress。
func (s Store) Claim(taskID string, owner string) (string, error) {
	task, err := s.Load(taskID)
	if err != nil {
		return "", err
	}

	if task.Status != StatusPending {
		return fmt.Sprintf(
			"Task %s is %s, cannot claim",
			taskID,
			task.Status,
		), nil
	}

	canStart, err := s.CanStart(taskID)
	if err != nil {
		return "", err
	}

	if !canStart {
		blocked := make([]string, 0)

		for _, dependencyID := range task.BlockedBy {
			dependency, err := s.Load(dependencyID)

			if os.IsNotExist(err) {
				blocked = append(blocked, dependencyID)
				continue
			}
			if err != nil {
				return "", err
			}

			if dependency.Status != StatusCompleted {
				blocked = append(blocked, dependencyID)
			}
		}

		raw, _ := json.Marshal(blocked)

		return "Blocked by: " + string(raw), nil
	}

	ownerCopy := owner
	task.Owner = &ownerCopy
	task.Status = StatusInProgress

	if err := s.Save(task); err != nil {
		return "", err
	}

	fmt.Printf(
		"  \033[36m[claim] %s → in_progress (owner: %s)\033[0m\n",
		task.Subject,
		owner,
	)

	return fmt.Sprintf(
		"Claimed %s (%s)",
		task.ID,
		task.Subject,
	), nil
}

// Complete 对标 Python complete_task。
//
// 将 in_progress 任务设为 completed，并报告当前已经满足依赖的下游 pending 任务。
func (s Store) Complete(taskID string) (string, error) {
	task, err := s.Load(taskID)
	if err != nil {
		return "", err
	}

	if task.Status != StatusInProgress {
		return fmt.Sprintf(
			"Task %s is %s, cannot complete",
			taskID,
			task.Status,
		), nil
	}

	task.Status = StatusCompleted

	if err := s.Save(task); err != nil {
		return "", err
	}

	fmt.Printf(
		"  \033[32m[complete] %s ✓\033[0m\n",
		task.Subject,
	)

	allTasks, err := s.List()
	if err != nil {
		return "", err
	}

	unblocked := make([]string, 0)

	for _, candidate := range allTasks {
		if candidate.Status != StatusPending ||
			len(candidate.BlockedBy) == 0 {
			continue
		}

		canStart, err := s.CanStart(candidate.ID)
		if err != nil {
			return "", err
		}

		if canStart {
			unblocked = append(unblocked, candidate.Subject)
		}
	}

	result := fmt.Sprintf(
		"Completed %s (%s)",
		task.ID,
		task.Subject,
	)

	if len(unblocked) > 0 {
		result += "\nUnblocked: " + strings.Join(unblocked, ", ")

		fmt.Printf(
			"  \033[33m[unblocked] %s\033[0m\n",
			strings.Join(unblocked, ", "),
		)
	}

	return result, nil
}

// taskPath 对标 Python _task_path。
//
// 将任务 ID 映射到 .tasks/{id}.json，并防止任务 ID 逃出任务目录。
func (s Store) taskPath(taskID string) (string, error) {
	taskID = strings.TrimSpace(taskID)

	if taskID == "" ||
		taskID == "." ||
		taskID == ".." ||
		filepath.Base(taskID) != taskID ||
		strings.ContainsAny(taskID, `/\`) {
		return "", fmt.Errorf("invalid task id %q", taskID)
	}

	return filepath.Join(s.Dir, taskID+".json"), nil
}
