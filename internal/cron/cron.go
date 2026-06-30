package cron

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// CronJob 对标 Python CronJob dataclass。
//
// 表示一个可被 cron 表达式触发、可选择持久化的计划任务。
type CronJob struct {
	ID        string `json:"id"`
	Cron      string `json:"cron"`
	Prompt    string `json:"prompt"`
	Recurring bool   `json:"recurring"`
	Durable   bool   `json:"durable"`
}

// Scheduler 对标 Python scheduled_jobs、cron_queue、cron_lock 和 _last_fired。
//
// 它只保存 cron 任务、触发队列和持久化文件路径，不持有模型客户端或 Agent Loop。
type Scheduler struct {
	mu          sync.Mutex
	durablePath string
	jobs        map[string]CronJob
	queue       []CronJob
	lastFired   map[string]string
}

// NewScheduler 对标 Python load_durable_jobs + scheduler 全局状态初始化。
//
// 创建当前工作区的 cron 调度器，并尽力加载 .scheduled_tasks.json。
func NewScheduler(workDir string) (*Scheduler, error) {
	workDir = strings.TrimSpace(workDir)
	if workDir == "" {
		return nil, fmt.Errorf("workDir is required")
	}

	scheduler := &Scheduler{
		durablePath: filepath.Join(workDir, ".scheduled_tasks.json"),
		jobs:        make(map[string]CronJob),
		queue:       make([]CronJob, 0),
		lastFired:   make(map[string]string),
	}

	if err := scheduler.LoadDurable(); err != nil {
		fmt.Printf("  \033[31m[cron] durable load skipped: %v\033[0m\n", err)
	}

	return scheduler, nil
}

// DurablePath 对标 Python DURABLE_PATH。
//
// 返回当前调度器使用的持久化文件路径，方便课程主流程打印或排查。
func (s *Scheduler) DurablePath() string {
	if s == nil {
		return ""
	}
	return s.durablePath
}

// Schedule 对标 Python schedule_job。
//
// 校验 cron 表达式，注册一个计划任务，并在 durable=true 时写入磁盘。
func (s *Scheduler) Schedule(
	cronExpr string,
	prompt string,
	recurring bool,
	durable bool,
) (CronJob, error) {
	if s == nil {
		return CronJob{}, fmt.Errorf("cron scheduler is nil")
	}

	cronExpr = strings.TrimSpace(cronExpr)
	prompt = strings.TrimSpace(prompt)

	if err := ValidateCron(cronExpr); err != nil {
		return CronJob{}, err
	}

	if prompt == "" {
		return CronJob{}, fmt.Errorf("prompt is required")
	}

	job := CronJob{
		ID:        fmt.Sprintf("cron_%06d", rand.Intn(1000000)),
		Cron:      cronExpr,
		Prompt:    prompt,
		Recurring: recurring,
		Durable:   durable,
	}

	s.mu.Lock()
	s.jobs[job.ID] = job

	/*原版写法是锁内写jobs，然后锁外实现文件输出，这里改成一致*/
	if durable {
		if err := s.saveDurableLocked(); err != nil {
			delete(s.jobs, job.ID)
			s.mu.Unlock()
			return CronJob{}, err
		}
	}

	s.mu.Unlock()

	fmt.Printf("  \033[35m[cron register] %s %q -> %s\033[0m\n", job.ID, job.Cron, previewRunes(job.Prompt, 40))

	return job, nil
}

// Cancel 对标 Python cancel_job。
//
// 取消指定 cron job 并直接返回用户可读结果；durable job 会同步更新磁盘文件。
func (s *Scheduler) Cancel(jobID string) string {
	jobID = strings.TrimSpace(jobID)
	s.mu.Lock()

	job, ok := s.jobs[jobID]
	if !ok {
		s.mu.Unlock()
		return fmt.Sprintf("Job %s not found", jobID)
	}
	lastFired, hadLastFired := s.lastFired[jobID]
	delete(s.jobs, jobID)
	delete(s.lastFired, jobID)
	if job.Durable {
		if err := s.saveDurableLocked(); err != nil {
			s.jobs[jobID] = job
			if hadLastFired {
				s.lastFired[jobID] = lastFired
			}
			s.mu.Unlock()
			return "Error: " + err.Error()
		}
	}
	s.mu.Unlock()
	fmt.Printf("  \033[31m[cron cancel] %s\033[0m\n", jobID)

	return fmt.Sprintf("Cancelled %s", jobID)
}

// List 对标 Python run_list_crons 读取 scheduled_jobs。
//
// 返回当前所有已注册 cron 任务，按 ID 稳定排序。
func (s *Scheduler) List() []CronJob {
	s.mu.Lock()
	defer s.mu.Unlock()

	ids := make([]string, 0, len(s.jobs))
	for id := range s.jobs {
		ids = append(ids, id)
	}

	sort.Strings(ids)

	jobs := make([]CronJob, 0, len(ids))
	for _, id := range ids {
		jobs = append(jobs, s.jobs[id])
	}

	return jobs
}

// StartScheduler 对标 Python cron_scheduler_loop。
//
// 独立 goroutine 每秒检查一次时间；命中时把 job 写入队列，不直接调用 agent_loop。
func StartScheduler(ctx context.Context, scheduler *Scheduler) {
	go func() {
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return

			case now := <-ticker.C:
				scheduler.FireDue(now)
			}
		}
	}()
}

// FireDue 对标 Python cron_scheduler_loop 中检查并触发任务的核心逻辑。
//
// 检查指定时间命中的任务，把它们放入队列，并用日期级 minute marker 防止同一分钟重复触发。
func (s *Scheduler) FireDue(now time.Time) int {
	minuteMarker := now.Format("2006-01-02 15:04")
	fired := 0
	saveNeeded := false

	s.mu.Lock()

	ids := make([]string, 0, len(s.jobs))
	for id := range s.jobs {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	for _, id := range ids {
		job, ok := s.jobs[id]
		if !ok {
			continue
		}

		if !CronMatches(job.Cron, now) {
			continue
		}

		if s.lastFired[job.ID] != minuteMarker {
			s.queue = append(s.queue, job)
			s.lastFired[job.ID] = minuteMarker
			fired++

			fmt.Printf(
				"  \033[35m[cron fire] %s -> %s\033[0m\n",
				job.ID,
				previewRunes(job.Prompt, 40),
			)
		}

		if !job.Recurring {
			delete(s.jobs, job.ID)
			delete(s.lastFired, job.ID)

			if job.Durable {
				saveNeeded = true
			}
		}
	}

	if saveNeeded {
		if err := s.saveDurableLocked(); err != nil {
			fmt.Printf("  \033[31m[cron] durable save skipped: %v\033[0m\n", err)
		}
	}

	s.mu.Unlock()

	return fired
}

// ConsumeQueue 对标 Python consume_cron_queue。
//
// 取出已经触发但尚未注入 Agent Loop 的 cron jobs，并清空队列。
func (s *Scheduler) ConsumeQueue() []CronJob {

	s.mu.Lock()
	defer s.mu.Unlock()

	fired := append([]CronJob(nil), s.queue...)
	s.queue = s.queue[:0]

	return fired
}

// HasQueue 对标 Python has_cron_queue。
//
// 供 queue processor 判断是否需要唤醒空闲 Agent。
func (s *Scheduler) HasQueue() bool {
	if s == nil {
		return false
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	return len(s.queue) > 0
}

// CronMatches 对标 Python cron_matches。
//
// 判断一个 5-field cron 表达式是否匹配给定时间；DOM 和 DOW 同时受限时使用 OR 语义。
func CronMatches(cronExpr string, dt time.Time) bool {
	fields := strings.Fields(cronExpr)
	if len(fields) != 5 {
		return false
	}

	minute := fields[0]
	hour := fields[1]
	dayOfMonth := fields[2]
	month := fields[3]
	dayOfWeek := fields[4]

	minuteOK := fieldMatches(minute, dt.Minute())
	hourOK := fieldMatches(hour, dt.Hour())
	dayOfMonthOK := fieldMatches(dayOfMonth, dt.Day())
	monthOK := fieldMatches(month, int(dt.Month()))
	dayOfWeekOK := fieldMatches(dayOfWeek, int(dt.Weekday()))

	if !(minuteOK && hourOK && monthOK) {
		return false
	}

	dayOfMonthUnconstrained := dayOfMonth == "*"
	dayOfWeekUnconstrained := dayOfWeek == "*"

	if dayOfMonthUnconstrained && dayOfWeekUnconstrained {
		return true
	}

	if dayOfMonthUnconstrained {
		return dayOfWeekOK
	}

	if dayOfWeekUnconstrained {
		return dayOfMonthOK
	}

	return dayOfMonthOK || dayOfWeekOK
}

// ValidateCron 对标 Python validate_cron。
//
// 校验 5-field cron 表达式的字段数量、数字范围、列表、步长和区间。
func ValidateCron(cronExpr string) error {
	fields := strings.Fields(cronExpr)
	if len(fields) != 5 {
		return fmt.Errorf("expected 5 fields, got %d", len(fields))
	}

	bounds := []struct {
		name string
		lo   int
		hi   int
	}{
		{name: "minute", lo: 0, hi: 59},
		{name: "hour", lo: 0, hi: 23},
		{name: "day-of-month", lo: 1, hi: 31},
		{name: "month", lo: 1, hi: 12},
		{name: "day-of-week", lo: 0, hi: 6},
	}

	for i, bound := range bounds {
		if err := validateField(fields[i], bound.lo, bound.hi); err != nil {
			return fmt.Errorf("%s: %s", bound.name, err)
		}
	}

	return nil
}

// LoadDurable 对标 Python load_durable_jobs。
//
// 启动时读取 .scheduled_tasks.json，跳过无效 cron 表达式，避免坏任务杀掉调度器。
func (s *Scheduler) LoadDurable() error {
	raw, err := os.ReadFile(s.durablePath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}

	if strings.TrimSpace(string(raw)) == "" {
		return nil
	}

	var jobs []CronJob
	if err := json.Unmarshal(raw, &jobs); err != nil {
		return err
	}

	validCount := 0

	s.mu.Lock()
	defer s.mu.Unlock()

	for _, job := range jobs {
		job.ID = strings.TrimSpace(job.ID)
		job.Cron = strings.TrimSpace(job.Cron)
		job.Prompt = strings.TrimSpace(job.Prompt)

		if job.ID == "" {
			fmt.Printf("  \033[31m[cron] skipping invalid job with empty id\033[0m\n")
			continue
		}

		if err := ValidateCron(job.Cron); err != nil {
			fmt.Printf("  \033[31m[cron] skipping invalid job %s: %v\033[0m\n", job.ID, err)
			continue
		}

		if job.Prompt == "" {
			fmt.Printf("  \033[31m[cron] skipping invalid job %s: empty prompt\033[0m\n", job.ID)
			continue
		}
		job.Durable = true
		s.jobs[job.ID] = job
		validCount++
	}

	if validCount > 0 {
		fmt.Printf("  \033[35m[cron] loaded %d durable job(s)\033[0m\n", validCount)
	}

	return nil
}

func (s *Scheduler) generateIDLocked() string {
	for attempt := 0; attempt < 1000; attempt++ {
		id := fmt.Sprintf("cron_%06d", rand.Intn(1000000))
		if _, exists := s.jobs[id]; !exists {
			return id
		}
	}
	return fmt.Sprintf("cron_%d", time.Now().UnixNano())
}
