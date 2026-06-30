package cron

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
)

// saveDurableLocked 对标 Python save_durable_jobs。
//
// 调用方必须已经持有 s.mu：S14 教学版把“内存任务表更新 + durable 文件写入”
// 放在同一个临界区里，避免 schedule/cancel 返回失败但内存状态已经生效，
// 也避免锁外旧快照晚写入时覆盖更新后的 .scheduled_tasks.json。
func (s *Scheduler) saveDurableLocked() error {
	durable := make([]CronJob, 0)

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

	// 这里故意在锁内写小 JSON 文件。相比缩短锁占用，当前课程更看重
	// “内存状态”和“重启后的磁盘状态”一致；生产优化可再引入 saveMu、
	// revision 或临时文件 + rename。
	return os.WriteFile(s.durablePath, raw, 0o600)
}
func validateField(field string, lo int, hi int) error {
	field = strings.TrimSpace(field)

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

// fieldMatches 对标 Python cron_matches 中的“单个 cron 字段命中判断”。
//
// 它不关心当前字段是 minute/hour/day/month/week 哪一列，只接收字段文本和当前时间值，
// 支持 "*"、"*/n"、"a,b,c"、"a-b" 和单个数字。逗号列表会递归复用同一套规则，
// 所以组合字段也能按 cron 语义判断。
//
// ValidateCron 已经负责完整边界校验；这里遇到坏字段直接返回 false，避免异常表达式
// 影响调度循环继续运行。
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
		lo, err1 := strconv.Atoi(parts[0])
		hi, err2 := strconv.Atoi(parts[1])

		return err1 == nil && err2 == nil && lo <= value && value <= hi
	}

	expected, err := strconv.Atoi(field)
	if err != nil {
		return false
	}

	return value == expected
}
