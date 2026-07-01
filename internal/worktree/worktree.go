package worktree

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"AgentLoop/internal/tasks"
)

var validName = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)

const gitTimeout = 30 * time.Second

// Store 对标 Python S18 WORKTREES_DIR。
//
// 迭代原因：S18 新增 worktree isolation，需要一个轻量对象稳定持有主仓库目录、
// .worktrees 目录和 events.jsonl 路径。
//
// 与旧实现差别：S17 没有隔离目录概念，所有工具都运行在主工作区；Store 只承载
// worktree 文件系统状态和 git 命令，不隐藏 main.go 的 Agent Loop。
type Store struct {
	WorkDir    string
	Dir        string
	EventsFile string
}

// Event 对标 Python S18 log_event 写入 events.jsonl 的记录。
//
// 记录 create/remove/keep 生命周期事件，方便学习时观察 worktree 的变化。
type Event struct {
	Type     string  `json:"type"`
	Worktree string  `json:"worktree"`
	TaskID   string  `json:"task_id"`
	Ts       float64 `json:"ts"`
}

// NewStore 对标 Python S18 WORKTREES_DIR.mkdir(exist_ok=True)。
//
// 初始化 .worktrees 目录；旧课程不调用它，因此不会改变 S17 之前的行为。
func NewStore(workDir string) (*Store, error) {
	workDir = strings.TrimSpace(workDir)
	if workDir == "" {
		return nil, fmt.Errorf("workDir is required")
	}

	absWorkDir, err := filepath.Abs(workDir)
	if err != nil {
		return nil, err
	}

	dir := filepath.Join(absWorkDir, ".worktrees")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}

	return &Store{
		WorkDir:    absWorkDir,
		Dir:        dir,
		EventsFile: filepath.Join(dir, "events.jsonl"),
	}, nil
}

// ValidateName 对标 Python S18 validate_worktree_name。
//
// 迭代原因：worktree name 会变成目录名和 branch 名，必须拒绝路径穿越和非法字符。
//
// 与普通 strings.TrimSpace 差别：这里显式拒绝空名、"."、".." 和非
// [A-Za-z0-9._-] 字符，避免 create/remove 工具逃出 .worktrees。
func ValidateName(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("worktree name cannot be empty")
	}
	if name == "." || name == ".." {
		return fmt.Errorf("%q is not a valid worktree name", name)
	}
	if !validName.MatchString(name) {
		return fmt.Errorf(
			"invalid worktree name %q: only letters, digits, dots, underscores, dashes (1-64 chars)",
			name,
		)
	}

	return nil
}

// Path 对标 Python S18 WORKTREES_DIR / name。
//
// 返回校验后的 worktree 绝对路径；所有 create/remove/cwd 切换都从这里取路径。
func (s *Store) Path(name string) (string, error) {
	if s == nil {
		return "", fmt.Errorf("worktree store is nil")
	}

	name = strings.TrimSpace(name)
	if err := ValidateName(name); err != nil {
		return "", err
	}

	return filepath.Join(s.Dir, name), nil
}

// RunGit 对标 Python S18 run_git。
//
// 迭代原因：S18 的 create/remove 都需要执行 git worktree 命令，并返回 (ok, output)
// 这种教学版结果，而不是直接 panic 或把错误吞掉。
//
// 与直接 exec.Command 差别：这里固定 cwd 为主仓库，统一 30 秒超时和 5000 rune 输出截断。
func (s *Store) RunGit(args ...string) (bool, string) {
	if s == nil {
		return false, "Error: worktree store is nil"
	}

	ctx, cancel := context.WithTimeout(context.Background(), gitTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = s.WorkDir

	raw, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return false, "Error: git timeout"
	}

	output := strings.TrimSpace(string(raw))
	if output == "" {
		output = "(no output)"
	}
	output = previewRunes(output, 5000)

	return err == nil, output
}

// LogEvent 对标 Python S18 log_event。
//
// create/remove/keep 成功后追加一行 JSONL；它只是课程观察日志，不参与恢复 Agent Loop。
func (s *Store) LogEvent(eventType string, worktreeName string, taskID string) error {
	if s == nil {
		return fmt.Errorf("worktree store is nil")
	}

	event := Event{
		Type:     eventType,
		Worktree: strings.TrimSpace(worktreeName),
		TaskID:   strings.TrimSpace(taskID),
		Ts:       float64(time.Now().UnixNano()) / 1e9,
	}

	raw, err := json.Marshal(event)
	if err != nil {
		return err
	}

	file, err := os.OpenFile(s.EventsFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()

	_, err = file.Write(append(raw, '\n'))

	return err
}

// Create 对标 Python S18 create_worktree。
//
// 迭代原因：Lead 需要为任务创建隔离 git worktree，并可选绑定 task_id。
//
// 与旧任务流程差别：这里只创建 .worktrees/{name} 和 wt/{name} branch；
// taskID 非空时只写 task.worktree，不自动 claim，保持 S17 autonomous teammate 的认领流程。
func (s *Store) Create(name string, taskID string, board tasks.Board) (string, error) {
	name = strings.TrimSpace(name)
	taskID = strings.TrimSpace(taskID)

	if err := ValidateName(name); err != nil {
		return "Error: " + err.Error(), nil
	}

	path, err := s.Path(name)
	if err != nil {
		return "", err
	}

	if _, err := os.Stat(path); err == nil {
		return fmt.Sprintf("Worktree %q already exists at %s", name, path), nil
	} else if !os.IsNotExist(err) {
		return "", err
	}

	ok, output := s.RunGit("worktree", "add", path, "-b", "wt/"+name, "HEAD")
	if !ok {
		return "Git error: " + output, nil
	}

	if taskID != "" {
		if err := board.BindWorktree(taskID, name); err != nil {
			return "", err
		}
	}

	if err := s.LogEvent("create", name, taskID); err != nil {
		return "", err
	}

	fmt.Printf(
		"  \033[33m[worktree] created: %s at %s\033[0m\n",
		name,
		path,
	)

	return fmt.Sprintf("Worktree %q created at %s", name, path), nil
}

// Remove 对标 Python S18 remove_worktree。
//
// 迭代原因：S18 要允许 Lead 清理隔离目录，但删除前必须保护未提交文件和未推送 commit。
//
// 与直接 git worktree remove 差别：默认先 CountChanges；只有 discardChanges=true 时才强制删除。
func (s *Store) Remove(name string, discardChanges bool) (string, error) {
	name = strings.TrimSpace(name)
	if err := ValidateName(name); err != nil {
		return "Error: " + err.Error(), nil
	}

	path, err := s.Path(name)
	if err != nil {
		return "", err
	}

	if _, err := os.Stat(path); os.IsNotExist(err) {
		return fmt.Sprintf("Worktree %q not found", name), nil
	} else if err != nil {
		return "", err
	}

	if !discardChanges {
		files, commits, err := s.CountChanges(path)
		if err != nil || files < 0 {
			return fmt.Sprintf(
				"Cannot verify worktree %q status. Use discard_changes=true to force removal.",
				name,
			), nil
		}

		if files > 0 || commits > 0 {
			return fmt.Sprintf(
				"Worktree %q has %d uncommitted file(s) and %d unpushed commit(s). Use discard_changes=true to force removal, or keep_worktree to preserve for review.",
				name,
				files,
				commits,
			), nil
		}
	}

	ok, _ := s.RunGit("worktree", "remove", path, "--force")
	if !ok {
		return fmt.Sprintf("Failed to remove worktree directory for %q", name), nil
	}

	_, _ = s.RunGit("branch", "-D", "wt/"+name)

	if err := s.LogEvent("remove", name, ""); err != nil {
		return "", err
	}

	fmt.Printf("  \033[33m[worktree] removed: %s\033[0m\n", name)

	return fmt.Sprintf("Worktree %q removed", name), nil
}

// Keep 对标 Python S18 keep_worktree。
//
// 迭代原因：当 worktree 有需要人工 review 的变更时，Lead 可以显式保留并记录事件。
//
// 与 Remove 差别：Keep 不删除目录、不删 branch，只写 keep 事件。
func (s *Store) Keep(name string) (string, error) {
	name = strings.TrimSpace(name)
	if err := ValidateName(name); err != nil {
		return "Error: " + err.Error(), nil
	}

	if err := s.LogEvent("keep", name, ""); err != nil {
		return "", err
	}

	fmt.Printf("  \033[36m[worktree] kept: %s\033[0m\n", name)

	return fmt.Sprintf("Worktree %q kept for review (branch: wt/%s)", name, name), nil
}

// CountChanges 对标 Python S18 _count_worktree_changes。
//
// 统计未提交文件数和 @{push}..HEAD commit 数；无法读取 git status 时返回 error。
func (s *Store) CountChanges(path string) (int, int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), gitTimeout)
	defer cancel()

	status := exec.CommandContext(ctx, "git", "status", "--porcelain")
	status.Dir = path

	rawStatus, err := status.Output()
	if err != nil {
		return -1, -1, err
	}

	files := countNonEmptyLines(string(rawStatus))

	logCtx, logCancel := context.WithTimeout(context.Background(), gitTimeout)
	defer logCancel()

	logCmd := exec.CommandContext(logCtx, "git", "log", "@{push}..HEAD", "--oneline")
	logCmd.Dir = path

	rawLog, _ := logCmd.Output()
	commits := countNonEmptyLines(string(rawLog))

	return files, commits, nil
}

func countNonEmptyLines(text string) int {
	count := 0

	for _, line := range strings.Split(strings.TrimSpace(text), "\n") {
		if strings.TrimSpace(line) != "" {
			count++
		}
	}

	return count
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
