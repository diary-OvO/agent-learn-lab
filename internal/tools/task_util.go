package tools

// TaskIDArgs 对标 Python run_get_task、run_claim_task、run_complete_task 的参数。
//
// 三个工具都只需要一个 task_id，因此复用同一个参数类型。
type TaskIDArgs struct {
	TaskID string `json:"task_id"`
}
