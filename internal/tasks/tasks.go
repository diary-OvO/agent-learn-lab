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

// Board 对标 Python TASKS_DIR。
//
// 它表示持久任务板这一稳定状态，负责保存任务并维护 claim/complete 任务图语义。
type Board struct {
	Dir string
}

// NewBoard 对标 Python TASKS_DIR.mkdir(exist_ok=True)。
//
// 创建并返回当前工作区的任务板目录。
func NewBoard(workDir string) (Board, error) {
	dir := filepath.Join(workDir, ".tasks")

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return Board{}, err
	}

	return Board{
		Dir: dir,
	}, nil
}

// Create 对标 Python create_task。
//
// 创建一个 pending 任务，生成简单任务 ID，并立即写入磁盘。
func (b Board) Create(
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

	if err := b.Save(task); err != nil {
		return Task{}, err
	}

	return task, nil
}

// Save 对标 Python save_task。
//
// 将完整 Task 以缩进 JSON 写入 .tasks/{id}.json。
func (b Board) Save(task Task) error {
	path, err := b.taskPath(task.ID)
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
func (b Board) Load(taskID string) (Task, error) {
	path, err := b.taskPath(taskID)
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
func (b Board) List() ([]Task, error) {
	entries, err := os.ReadDir(b.Dir)
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

		task, err := b.Load(taskID)
		if err != nil {
			return nil, err
		}

		result = append(result, task)
	}

	return result, nil
}

// ScanUnclaimed 对标 Python S17 scan_unclaimed_tasks。
//
// 查找 pending、无 owner、且 blockedBy 依赖全部 completed 的任务，供 autonomous teammate 空闲时自动认领。
// 迭代原因：S12-S16 的任务板只能被工具显式 list/claim，teammate 空闲时没有“自己找活”的入口。
// 与旧函数差别：List 返回全部任务给模型判断；ScanUnclaimed 只返回可自动认领的最小候选集，避免 S17 idle loop 把 blocked 或已有 owner 的任务误当成工作。
func (b Board) ScanUnclaimed() ([]Task, error) {
	allTasks, err := b.List()
	if err != nil {
		return nil, err
	}

	result := make([]Task, 0)

	for _, task := range allTasks {
		if task.Status != StatusPending {
			continue
		}

		if task.Owner != nil && strings.TrimSpace(*task.Owner) != "" {
			continue
		}

		canStart, err := b.CanStart(task.ID)
		if err != nil {
			return nil, err
		}

		if canStart {
			result = append(result, task)
		}
	}

	return result, nil
}

// Get 对标 Python get_task。
//
// 返回单个任务的完整缩进 JSON，供 Agent 跨会话恢复任务细节。
func (b Board) Get(taskID string) (string, error) {
	task, err := b.Load(taskID)
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
func (b Board) CanStart(taskID string) (bool, error) {
	task, err := b.Load(taskID)
	if err != nil {
		return false, err
	}

	for _, dependencyID := range task.BlockedBy {
		dependency, err := b.Load(dependencyID)

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
func (b Board) Claim(taskID string, owner string) (string, error) {
	task, err := b.Load(taskID)
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

	canStart, err := b.CanStart(taskID)
	if err != nil {
		return "", err
	}

	if !canStart {
		blocked := make([]string, 0)

		for _, dependencyID := range task.BlockedBy {
			dependency, err := b.Load(dependencyID)

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

	if err := b.Save(task); err != nil {
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

// ClaimWithOwnerCheck 对标 Python S17 claim_task。
//
// S17 新增 owner 检查：任务如果已经有 owner，就不能被 autonomous teammate 覆盖认领。
// 旧的 Claim 保持原语义，供 S12-S16 课程入口继续按原逻辑使用。
// 迭代原因：S17 teammate 会并发 idle-poll 任务板，多个 teammate 可能看到同一个 pending task。
// 与旧函数差别：Claim 只检查 status/dependency 并写入 owner；ClaimWithOwnerCheck 额外拒绝已经有 owner 的任务，用于 autonomous auto-claim 的并发保护。
func (b Board) ClaimWithOwnerCheck(taskID string, owner string) (string, error) {
	task, err := b.Load(taskID)
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

	if task.Owner != nil && strings.TrimSpace(*task.Owner) != "" {
		return fmt.Sprintf(
			"Task %s already owned by %s",
			taskID,
			*task.Owner,
		), nil
	}

	canStart, err := b.CanStart(taskID)
	if err != nil {
		return "", err
	}

	if !canStart {
		blocked := make([]string, 0)

		for _, dependencyID := range task.BlockedBy {
			dependency, err := b.Load(dependencyID)

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

	ownerCopy := strings.TrimSpace(owner)
	if ownerCopy == "" {
		ownerCopy = "agent"
	}

	task.Owner = &ownerCopy
	task.Status = StatusInProgress

	if err := b.Save(task); err != nil {
		return "", err
	}

	fmt.Printf(
		"  \033[36m[claim] %s → in_progress (owner: %s)\033[0m\n",
		task.Subject,
		ownerCopy,
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
func (b Board) Complete(taskID string) (string, error) {
	task, err := b.Load(taskID)
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

	if err := b.Save(task); err != nil {
		return "", err
	}

	fmt.Printf(
		"  \033[32m[complete] %s ✓\033[0m\n",
		task.Subject,
	)

	allTasks, err := b.List()
	if err != nil {
		return "", err
	}

	unblocked := make([]string, 0)

	for _, candidate := range allTasks {
		if candidate.Status != StatusPending ||
			len(candidate.BlockedBy) == 0 {
			continue
		}

		canStart, err := b.CanStart(candidate.ID)
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
func (b Board) taskPath(taskID string) (string, error) {
	taskID = strings.TrimSpace(taskID)

	if taskID == "" ||
		taskID == "." ||
		taskID == ".." ||
		filepath.Base(taskID) != taskID ||
		strings.ContainsAny(taskID, `/\`) {
		return "", fmt.Errorf("invalid task id %q", taskID)
	}

	return filepath.Join(b.Dir, taskID+".json"), nil
}
