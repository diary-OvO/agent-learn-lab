package team

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

const (
	ProtocolShutdown     = "shutdown"
	ProtocolPlanApproval = "plan_approval"

	ProtocolPending  = "pending"
	ProtocolApproved = "approved"
	ProtocolRejected = "rejected"
)

// ProtocolState 对标 Python ProtocolState dataclass。
//
// 记录一个 Lead/team 之间正在等待响应的协议请求。
type ProtocolState struct {
	RequestID string  `json:"request_id"`
	Type      string  `json:"type"`
	Sender    string  `json:"sender"`
	Target    string  `json:"target"`
	Status    string  `json:"status"`
	Payload   string  `json:"payload"`
	CreatedAt float64 `json:"created_at"`
}

// ProtocolBook 对标 Python pending_requests dict。
//
// 它只保存内存中的 request_id -> ProtocolState，不持久化，不接管 Agent Loop。
type ProtocolBook struct {
	mu      sync.Mutex
	counter int
	pending map[string]ProtocolState
}

// NewProtocolBook 对标 Python pending_requests 初始化。
//
// 创建当前进程内的协议请求记录表。
func NewProtocolBook() *ProtocolBook {
	return &ProtocolBook{
		pending: make(map[string]ProtocolState),
	}
}

// NewRequestID 对标 Python new_request_id。
//
// 生成教学版 request_id；只需在当前进程内唯一。
func (b *ProtocolBook) NewRequestID() string {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.counter++

	return fmt.Sprintf("req_%06d", b.counter)
}

// Add 对标 Python pending_requests[req_id] = ProtocolState(...)。
//
// 注册一个等待后续 response 的协议请求。
func (b *ProtocolBook) Add(state ProtocolState) {
	if b == nil {
		return
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	if state.CreatedAt == 0 {
		state.CreatedAt = float64(time.Now().UnixNano()) / 1e9
	}
	if state.Status == "" {
		state.Status = ProtocolPending
	}

	b.pending[state.RequestID] = state
}

// MatchResponse 对标 Python match_response。
//
// 通过 request_id 关联响应，并验证 response type 与原请求类型匹配。
func (b *ProtocolBook) MatchResponse(
	responseType string,
	requestID string,
	approve bool,
) string {
	if b == nil {
		return ""
	}

	requestID = strings.TrimSpace(requestID)
	responseType = strings.TrimSpace(responseType)

	if requestID == "" {
		return ""
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	state, ok := b.pending[requestID]
	if !ok {
		msg := fmt.Sprintf("[protocol] unknown request_id: %s", requestID)
		fmt.Printf("  \033[31m%s\033[0m\n", msg)

		return msg
	}

	expected := ""
	switch state.Type {
	case ProtocolShutdown:
		expected = "shutdown_response"
	case ProtocolPlanApproval:
		expected = "plan_approval_response"
	}

	if expected != "" && responseType != expected {
		msg := fmt.Sprintf(
			"[protocol] type mismatch: expected %s, got %s",
			expected,
			responseType,
		)
		fmt.Printf("  \033[31m%s\033[0m\n", msg)

		return msg
	}

	if state.Status != ProtocolPending {
		msg := fmt.Sprintf(
			"[protocol] %s already %s, ignoring duplicate",
			requestID,
			state.Status,
		)
		fmt.Printf("  \033[33m%s\033[0m\n", msg)

		return msg
	}

	if approve {
		state.Status = ProtocolApproved
	} else {
		state.Status = ProtocolRejected
	}

	b.pending[requestID] = state

	icon := "✓"
	color := "32"
	if !approve {
		icon = "✗"
		color = "31"
	}

	msg := fmt.Sprintf(
		"[protocol] %s %s (%s: %s)",
		state.Type,
		icon,
		requestID,
		state.Status,
	)

	fmt.Printf("  \033[%sm%s\033[0m\n", color, msg)

	return msg
}

// RequestShutdown 对标 Python run_request_shutdown。
//
// Lead 发送 shutdown_request，并登记 pending shutdown 请求。
func (b *ProtocolBook) RequestShutdown(
	bus *MessageBus,
	teammate string,
) (string, error) {
	teammate = strings.TrimSpace(teammate)
	if teammate == "" {
		return "", fmt.Errorf("teammate is required")
	}

	reqID := b.NewRequestID()

	b.Add(ProtocolState{
		RequestID: reqID,
		Type:      ProtocolShutdown,
		Sender:    "lead",
		Target:    teammate,
		Status:    ProtocolPending,
		Payload:   "",
	})

	if err := bus.SendWithMetadata(
		"lead",
		teammate,
		"Please shut down gracefully.",
		"shutdown_request",
		map[string]any{
			"request_id": reqID,
		},
	); err != nil {
		return "", err
	}

	fmt.Printf(
		"  \033[35m[protocol] shutdown_request → %s (%s)\033[0m\n",
		teammate,
		reqID,
	)

	return fmt.Sprintf(
		"Shutdown request sent to %s (req: %s)",
		teammate,
		reqID,
	), nil
}

// RequestPlan 对标 Python run_request_plan。
//
// Lead 请求 teammate 提交计划；真正 pending 的 plan_approval 由 teammate submit_plan 创建。
func (b *ProtocolBook) RequestPlan(
	bus *MessageBus,
	teammate string,
	task string,
) (string, error) {
	teammate = strings.TrimSpace(teammate)
	task = strings.TrimSpace(task)

	if teammate == "" {
		return "", fmt.Errorf("teammate is required")
	}
	if task == "" {
		return "", fmt.Errorf("task is required")
	}

	if err := bus.Send(
		"lead",
		teammate,
		"Please submit a plan for: "+task,
		"message",
	); err != nil {
		return "", err
	}

	return fmt.Sprintf("Asked %s to submit a plan", teammate), nil
}

// SubmitPlan 对标 Python _teammate_submit_plan。
//
// teammate 创建 plan_approval 请求，并发送给 Lead 等待 review_plan。
func (b *ProtocolBook) SubmitPlan(
	bus *MessageBus,
	fromName string,
	plan string,
) (string, error) {
	fromName = strings.TrimSpace(fromName)
	plan = strings.TrimSpace(plan)

	if fromName == "" {
		return "", fmt.Errorf("from agent is required")
	}
	if plan == "" {
		return "", fmt.Errorf("plan is required")
	}

	reqID := b.NewRequestID()

	b.Add(ProtocolState{
		RequestID: reqID,
		Type:      ProtocolPlanApproval,
		Sender:    fromName,
		Target:    "lead",
		Status:    ProtocolPending,
		Payload:   plan,
	})

	if err := bus.SendWithMetadata(
		fromName,
		"lead",
		plan,
		"plan_approval_request",
		map[string]any{
			"request_id": reqID,
		},
	); err != nil {
		return "", err
	}

	return fmt.Sprintf(
		"Plan submitted (%s). Waiting for approval...",
		reqID,
	), nil
}

// ReviewPlan 对标 Python run_review_plan。
//
// Lead 根据 request_id 批准或拒绝 teammate 提交的计划。
func (b *ProtocolBook) ReviewPlan(
	bus *MessageBus,
	requestID string,
	approve bool,
	feedback string,
) (string, error) {
	if b == nil {
		return "", fmt.Errorf("protocol book is nil")
	}

	requestID = strings.TrimSpace(requestID)
	if requestID == "" {
		return "", fmt.Errorf("request_id is required")
	}

	b.mu.Lock()
	state, ok := b.pending[requestID]
	if !ok {
		b.mu.Unlock()
		return fmt.Sprintf("Request %s not found", requestID), nil
	}

	if state.Status != ProtocolPending {
		status := state.Status
		b.mu.Unlock()
		return fmt.Sprintf("Request %s already %s", requestID, status), nil
	}

	if approve {
		state.Status = ProtocolApproved
	} else {
		state.Status = ProtocolRejected
	}

	b.pending[requestID] = state
	b.mu.Unlock()

	content := strings.TrimSpace(feedback)
	if content == "" {
		if approve {
			content = "Approved"
		} else {
			content = "Rejected"
		}
	}

	if err := bus.SendWithMetadata(
		"lead",
		state.Sender,
		content,
		"plan_approval_response",
		map[string]any{
			"request_id": requestID,
			"approve":    approve,
		},
	); err != nil {
		return "", err
	}

	icon := "✓"
	if !approve {
		icon = "✗"
	}

	fmt.Printf(
		"  \033[32m[protocol] plan %s (%s)\033[0m\n",
		icon,
		requestID,
	)

	if approve {
		return fmt.Sprintf("Plan approved (%s)", requestID), nil
	}

	return fmt.Sprintf("Plan rejected (%s)", requestID), nil
}

// ConsumeLeadInbox 对标 Python consume_lead_inbox(route_protocol=True)。
//
// Lead 读取 inbox 时，先路由协议 response，再把消息返回给 Agent 历史注入。
func ConsumeLeadInbox(
	bus *MessageBus,
	book *ProtocolBook,
) ([]Message, error) {
	if bus == nil {
		return nil, nil
	}

	messages, err := bus.ReadInbox("lead")
	if err != nil {
		return nil, err
	}

	for _, msg := range messages {
		reqID := MetaString(msg.Metadata, "request_id")
		if reqID == "" || !strings.HasSuffix(msg.Type, "_response") {
			continue
		}

		approve := metaBool(msg.Metadata, "approve")

		if book != nil {
			book.MatchResponse(msg.Type, reqID, approve)
		}
	}

	return messages, nil
}

func MetaString(meta map[string]any, key string) string {
	if meta == nil {
		return ""
	}

	value, ok := meta[key]
	if !ok {
		return ""
	}

	switch v := value.(type) {
	case string:
		return strings.TrimSpace(v)
	default:
		return strings.TrimSpace(fmt.Sprint(v))
	}
}

func metaBool(meta map[string]any, key string) bool {
	if meta == nil {
		return false
	}

	value, ok := meta[key]
	if !ok {
		return false
	}

	switch v := value.(type) {
	case bool:
		return v
	case string:
		return strings.EqualFold(v, "true")
	default:
		return false
	}
}
