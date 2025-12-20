package server

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
)

// SSESubscriber SSE 订阅者实现
type SSESubscriber struct {
	writer  http.ResponseWriter
	request *http.Request
	mu      sync.Mutex
	closed  bool
}

// NewSSESubscriber 创建新的 SSE 订阅者
func NewSSESubscriber(w http.ResponseWriter, r *http.Request) *SSESubscriber {
	return &SSESubscriber{
		writer:  w,
		request: r,
		closed:  false,
	}
}

// SendEvent 发送事件给订阅者
func (s *SSESubscriber) SendEvent(event *OperationEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return fmt.Errorf("订阅者已关闭")
	}

	// 将事件序列化为 JSON
	eventJSON, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("序列化事件失败: %w", err)
	}

	// 发送 SSE 格式的数据
	// SSE 格式: data: {json}\n\n
	data := fmt.Sprintf("data: %s\n\n", string(eventJSON))

	// 使用 Flusher 立即发送数据
	if flusher, ok := s.writer.(http.Flusher); ok {
		_, err = fmt.Fprint(s.writer, data)
		if err != nil {
			s.closed = true
			return fmt.Errorf("发送数据失败: %w", err)
		}
		flusher.Flush()
		log.Printf("[SSESubscriber] 发送事件: type=%s, resource_id=%s, success=%v",
			event.Type, event.ResourceID, event.Success)
		return nil
	}

	return fmt.Errorf("ResponseWriter 不支持 Flush")
}

// Close 关闭订阅者连接
func (s *SSESubscriber) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.closed {
		s.closed = true
		log.Printf("[SSESubscriber] 关闭订阅者连接")
	}
}
