package team

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Message 对标 Python MessageBus 写入 .mailboxes/*.jsonl 的单行 JSON。
//
// 表示一个 agent 发给另一个 agent 的 inbox 消息。
type Message struct {
	From     string         `json:"from"`
	To       string         `json:"to"`
	Content  string         `json:"content"`
	Type     string         `json:"type"`
	Ts       float64        `json:"ts"`
	Metadata map[string]any `json:"metadata"`
}

// MessageBus 对标 Python MessageBus。
//
// 使用 .mailboxes/{agent}.jsonl 做教学版文件邮箱；ReadInbox 是破坏性读取。
type MessageBus struct {
	dir string
	mu  sync.Mutex
}

// NewMessageBus 对标 Python MAILBOX_DIR.mkdir(exist_ok=True)。
//
// 创建当前工作区下的 .mailboxes 目录。
func NewMessageBus(workDir string) (*MessageBus, error) {
	dir := filepath.Join(workDir, ".mailboxes")

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}

	return &MessageBus{
		dir: dir,
	}, nil
}

// Send 对标 S15 MessageBus.send 的普通消息形式。
//
// 不带 metadata，适合普通 teammate 消息。
func (b *MessageBus) Send(
	fromAgent string,
	toAgent string,
	content string,
	msgType string,
) error {
	return b.SendWithMetadata(fromAgent, toAgent, content, msgType, nil)
}

// SendWithMetadata 对标 S16 MessageBus.send(..., metadata={...})。
//
// 用于发送 shutdown_request、plan_approval_response 等带 request_id 的协议消息。
func (b *MessageBus) SendWithMetadata(
	fromAgent string,
	toAgent string,
	content string,
	msgType string,
	metadata map[string]any,
) error {

	fromAgent = strings.TrimSpace(fromAgent)
	toAgent = strings.TrimSpace(toAgent)
	content = strings.TrimSpace(content)
	msgType = strings.TrimSpace(msgType)

	if fromAgent == "" {
		fromAgent = "unknown"
	}
	if toAgent == "" {
		return fmt.Errorf("to agent is required")
	}
	if msgType == "" {
		msgType = "message"
	}

	msg := Message{
		From:     fromAgent,
		To:       toAgent,
		Content:  content,
		Type:     msgType,
		Ts:       float64(time.Now().UnixNano()) / 1e9,
		Metadata: metadata,
	}

	raw, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	inboxPath, err := b.inboxPath(toAgent)
	if err != nil {
		return err
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	f, err := os.OpenFile(inboxPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()

	if _, err := f.Write(append(raw, '\n')); err != nil {
		return err
	}

	fmt.Printf(
		"  \033[33m[bus] %s → %s: (%s) %s\033[0m\n",
		fromAgent,
		toAgent,
		msgType,
		previewRunes(content, 50),
	)

	return nil
}

// ReadInbox 对标 Python MessageBus.read_inbox。
//
// 读取并删除指定 agent 的 inbox；这是教学版 destructive consume。
func (b *MessageBus) ReadInbox(agent string) ([]Message, error) {
	if b == nil {
		return nil, nil
	}

	inboxPath, err := b.inboxPath(agent)
	if err != nil {
		return nil, err
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	raw, err := os.ReadFile(inboxPath)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	_ = os.Remove(inboxPath)

	lines := strings.Split(string(raw), "\n")
	messages := make([]Message, 0, len(lines))

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		var msg Message
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			continue
		}

		messages = append(messages, msg)
	}

	return messages, nil
}

// Peek 对标 Python MessageBus.peek。
//
// 非破坏性检查某个 agent 是否有未读消息，用于唤醒 Lead 回合。
func (b *MessageBus) Peek(agent string) bool {
	if b == nil {
		return false
	}

	inboxPath, err := b.inboxPath(agent)
	if err != nil {
		return false
	}

	info, err := os.Stat(inboxPath)
	if err != nil {
		return false
	}

	return info.Size() > 0
}

func (b *MessageBus) inboxPath(agent string) (string, error) {
	agent = strings.TrimSpace(agent)
	if agent == "" ||
		filepath.Base(agent) != agent ||
		strings.ContainsAny(agent, `/\`) {
		return "", fmt.Errorf("invalid agent name %q", agent)
	}

	return filepath.Join(b.dir, agent+".jsonl"), nil
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
