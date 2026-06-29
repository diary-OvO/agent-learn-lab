package cron

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	DurableFilename    = ".scheduled_tasks.json"
	minuteMarkerLayout = "2006-01-02 15:04"
)

// Job 对标 Python CronJob dataclass。
//
// 表示一个可被 cron 表达式触发、可选择持久化的计划任务。
type Job struct {
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
	jobs        map[string]Job
	queue       []Job
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
		durablePath: filepath.Join(workDir, DurableFilename),
		jobs:        make(map[string]Job),
		queue:       make([]Job, 0),
		lastFired:   make(map[string]string),
	}

	if err := scheduler.loadDurable(); err != nil {
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
) (Job, error) {
	if s == nil {
		return Job{}, fmt.Errorf("cron scheduler is nil")
	}

	cronExpr = strings.TrimSpace(cronExpr)
	prompt = strings.TrimSpace(prompt)

	if err := Validate(cronExpr); err != nil {
		return Job{}, err
	}

	if prompt == "" {
		return Job{}, fmt.Errorf("prompt is required")
	}

	s.mu.Lock()

	job := Job{
		ID:        s.generateIDLocked(),
		Cron:      cronExpr,
		Prompt:    prompt,
		Recurring: recurring,
		Durable:   durable,
	}

	s.jobs[job.ID] = job

	if durable {
		if err := s.saveDurableLocked(); err != nil {
			delete(s.jobs, job.ID)
			s.mu.Unlock()
			return Job{}, err
		}
	}

	s.mu.Unlock()

	fmt.Printf(
		"  \033[35m[cron register] %s %q -> %s\033[0m\n",
		job.ID,
		job.Cron,
		previewRunes(job.Prompt, 40),
	)

	return job, nil
}

// Cancel 对标 Python cancel_job。
//
// 删除指定 cron 任务；如果任务是 durable，则同步更新 .scheduled_tasks.json。
func (s *Scheduler) Cancel(jobID string) (Job, bool, error) {
	if s == nil {
		return Job{}, false, fmt.Errorf("cron scheduler is nil")
	}

	jobID = strings.TrimSpace(jobID)
	if jobID == "" {
		return Job{}, false, fmt.Errorf("job_id is required")
	}

	s.mu.Lock()

	job, ok := s.jobs[jobID]
	if !ok {
		s.mu.Unlock()
		return Job{}, false, nil
	}

	delete(s.jobs, jobID)
	delete(s.lastFired, jobID)

	if job.Durable {
		if err := s.saveDurableLocked(); err != nil {
			s.jobs[jobID] = job
			s.mu.Unlock()
			return Job{}, false, err
		}
	}

	s.mu.Unlock()

	fmt.Printf("  \033[31m[cron cancel] %s\033[0m\n", jobID)

	return job, true, nil
}

// List 对标 Python run_list_crons 读取 scheduled_jobs。
//
// 返回当前所有已注册 cron 任务，按 ID 稳定排序。
func (s *Scheduler) List() []Job {
	if s == nil {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	jobs := make([]Job, 0, len(s.jobs))
	for _, job := range s.jobs {
		jobs = append(jobs, job)
	}

	sort.Slice(jobs, func(i int, j int) bool {
		return jobs[i].ID < jobs[j].ID
	})

	return jobs
}

// FireDue 对标 Python cron_scheduler_loop 中检查并触发任务的核心逻辑。
//
// 检查指定时间命中的任务，把它们放入队列，并用日期级 minute marker 防止同一分钟重复触发。
func (s *Scheduler) FireDue(now time.Time) int {
	if s == nil {
		return 0
	}

	minuteMarker := now.Format(minuteMarkerLayout)
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

		didFire, removeJob := s.fireJobLocked(job, now, minuteMarker)
		if didFire {
			fired++
		}

		if removeJob {
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

// Consume 对标 Python consume_cron_queue。
//
// 取出已经触发但尚未注入 Agent Loop 的 cron jobs，并清空队列。
func (s *Scheduler) Consume() []Job {
	if s == nil {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	fired := append([]Job(nil), s.queue...)
	s.queue = s.queue[:0]

	return fired
}

// HasQueued 对标 Python has_cron_queue。
//
// 供 queue processor 判断是否需要唤醒空闲 Agent。
func (s *Scheduler) HasQueued() bool {
	if s == nil {
		return false
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	return len(s.queue) > 0
}

// Matches 对标 Python cron_matches。
//
// 判断一个 5-field cron 表达式是否匹配给定时间；DOM 和 DOW 同时受限时使用 OR 语义。
func Matches(cronExpr string, dt time.Time) bool {
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

// Validate 对标 Python validate_cron。
//
// 校验 5-field cron 表达式的字段数量、数字范围、列表、步长和区间。
func Validate(cronExpr string) error {
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

func (s *Scheduler) fireJobLocked(
	job Job,
	now time.Time,
	minuteMarker string,
) (didFire bool, removeJob bool) {
	defer func() {
		if r := recover(); r != nil {
			fmt.Printf("  \033[31m[cron error] %s: %v\033[0m\n", job.ID, r)
			didFire = false
			removeJob = false
		}
	}()

	if !Matches(job.Cron, now) {
		return false, false
	}

	if s.lastFired[job.ID] == minuteMarker {
		return false, false
	}

	s.queue = append(s.queue, job)
	s.lastFired[job.ID] = minuteMarker

	fmt.Printf(
		"  \033[35m[cron fire] %s -> %s\033[0m\n",
		job.ID,
		previewRunes(job.Prompt, 40),
	)

	return true, !job.Recurring
}

func (s *Scheduler) loadDurable() error {
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

	var jobs []Job
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

		if err := Validate(job.Cron); err != nil {
			fmt.Printf("  \033[31m[cron] skipping invalid job %s: %v\033[0m\n", job.ID, err)
			continue
		}

		if job.Prompt == "" {
			fmt.Printf("  \033[31m[cron] skipping invalid job %s: empty prompt\033[0m\n", job.ID)
			continue
		}

		s.jobs[job.ID] = job
		validCount++
	}

	if validCount > 0 {
		fmt.Printf("  \033[35m[cron] loaded %d durable job(s)\033[0m\n", validCount)
	}

	return nil
}

func (s *Scheduler) saveDurableLocked() error {
	durable := make([]Job, 0)

	for _, job := range s.jobs {
		if job.Durable {
			durable = append(durable, job)
		}
	}

	sort.Slice(durable, func(i int, j int) bool {
		return durable[i].ID < durable[j].ID
	})

	raw, err := json.MarshalIndent(durable, "", "  ")
	if err != nil {
		return err
	}

	raw = append(raw, '\n')

	return os.WriteFile(s.durablePath, raw, 0o600)
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

func fieldMatches(field string, value int) bool {
	field = strings.TrimSpace(field)

	if field == "" {
		return false
	}

	if field == "*" {
		return true
	}

	if strings.HasPrefix(field, "*/") {
		step, err := strconv.Atoi(field[2:])
		return err == nil && step > 0 && value%step == 0
	}

	if strings.Contains(field, ",") {
		parts := strings.Split(field, ",")
		for _, part := range parts {
			if fieldMatches(part, value) {
				return true
			}
		}

		return false
	}

	if strings.Contains(field, "-") {
		parts := strings.SplitN(field, "-", 2)
		if len(parts) != 2 {
			return false
		}

		lo, err := strconv.Atoi(strings.TrimSpace(parts[0]))
		if err != nil {
			return false
		}

		hi, err := strconv.Atoi(strings.TrimSpace(parts[1]))
		if err != nil {
			return false
		}

		return lo <= value && value <= hi
	}

	expected, err := strconv.Atoi(field)
	if err != nil {
		return false
	}

	return value == expected
}

func validateField(field string, lo int, hi int) error {
	field = strings.TrimSpace(field)

	if field == "" {
		return fmt.Errorf("empty field")
	}

	if field == "*" {
		return nil
	}

	if strings.HasPrefix(field, "*/") {
		step, err := strconv.Atoi(field[2:])
		if err != nil {
			return fmt.Errorf("invalid step: %s", field)
		}

		if step <= 0 {
			return fmt.Errorf("step must be > 0: %s", field)
		}

		return nil
	}

	if strings.Contains(field, ",") {
		parts := strings.Split(field, ",")
		for _, part := range parts {
			part = strings.TrimSpace(part)
			if part == "" {
				return fmt.Errorf("invalid list: %s", field)
			}

			if err := validateField(part, lo, hi); err != nil {
				return err
			}
		}

		return nil
	}

	if strings.Contains(field, "-") {
		parts := strings.SplitN(field, "-", 2)
		if len(parts) != 2 {
			return fmt.Errorf("invalid range: %s", field)
		}

		start, err := strconv.Atoi(strings.TrimSpace(parts[0]))
		if err != nil {
			return fmt.Errorf("invalid range: %s", field)
		}

		end, err := strconv.Atoi(strings.TrimSpace(parts[1]))
		if err != nil {
			return fmt.Errorf("invalid range: %s", field)
		}

		if start < lo || start > hi || end < lo || end > hi {
			return fmt.Errorf("range %s out of bounds [%d-%d]", field, lo, hi)
		}

		if start > end {
			return fmt.Errorf("range start > end: %s", field)
		}

		return nil
	}

	value, err := strconv.Atoi(field)
	if err != nil {
		return fmt.Errorf("invalid field: %s", field)
	}

	if value < lo || value > hi {
		return fmt.Errorf("value %d out of bounds [%d-%d]", value, lo, hi)
	}

	return nil
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
