package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/mux"
)

// TestSubscriberPattern 测试订阅者模式
func TestSubscriberPattern(t *testing.T) {
	lm := NewLockManager(true)
	handler := NewHandler(lm)

	// 创建测试服务器
	muxRouter := mux.NewRouter()
	handler.RegisterRoutes(muxRouter)
	router := httptest.NewServer(muxRouter)
	defer router.Close()

	resourceID := "sha256:test123"
	lockType := OperationTypePull

	// 1. 创建订阅者（模拟客户端订阅）
	subscribeURL := router.URL + "/lock/subscribe?type=" + lockType + "&resource_id=" + resourceID
	req, err := http.NewRequest("GET", subscribeURL, nil)
	if err != nil {
		t.Fatalf("创建订阅请求失败: %v", err)
	}

	// 创建响应记录器
	subscriberRecorder := httptest.NewRecorder()

	// 在 goroutine 中处理订阅请求（SSE 是长连接）
	subscriberDone := make(chan bool)
	var receivedEvents []OperationEvent

	go func() {
		defer close(subscriberDone)
		handler.Subscribe(subscriberRecorder, req)

		// 解析接收到的 SSE 事件
		body := subscriberRecorder.Body.String()
		lines := strings.Split(body, "\n")
		for _, line := range lines {
			if strings.HasPrefix(line, "data: ") {
				eventJSON := strings.TrimPrefix(line, "data: ")
				var event OperationEvent
				if err := json.Unmarshal([]byte(eventJSON), &event); err == nil {
					receivedEvents = append(receivedEvents, event)
				}
			}
		}
	}()

	// 等待订阅者注册完成
	time.Sleep(100 * time.Millisecond)

	// 2. 执行操作：获取锁并释放（成功）
	nodeID := "node-1"
	lockReq := &LockRequest{
		Type:       lockType,
		ResourceID: resourceID,
		NodeID:     nodeID,
	}
	acquired, _, _ := lm.TryLock(lockReq)
	if !acquired {
		t.Fatalf("无法获取锁")
	}

	// 模拟操作时间
	time.Sleep(50 * time.Millisecond)

	// 3. 释放锁（操作成功）
	unlockReq := &UnlockRequest{
		Type:       lockType,
		ResourceID: resourceID,
		NodeID:     nodeID,
		Error:      "", // 空字符串表示操作成功
	}
	lm.Unlock(unlockReq)

	// 等待事件广播
	time.Sleep(300 * time.Millisecond)

	// 4. 验证订阅者是否收到事件
	// 解析接收到的 SSE 事件
	body := subscriberRecorder.Body.String()
	if body == "" {
		t.Error("订阅者未收到任何数据")
		return
	}

	lines := strings.Split(body, "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "data: ") {
			eventJSON := strings.TrimPrefix(line, "data: ")
			var event OperationEvent
			if err := json.Unmarshal([]byte(eventJSON), &event); err == nil {
				receivedEvents = append(receivedEvents, event)
			}
		}
	}

	if len(receivedEvents) == 0 {
		t.Errorf("订阅者未收到事件，响应内容: %s", body)
	} else {
		event := receivedEvents[0]
		if event.Type != lockType {
			t.Errorf("事件类型不匹配: 期望 %s, 实际 %s", lockType, event.Type)
		}
		if event.ResourceID != resourceID {
			t.Errorf("资源ID不匹配: 期望 %s, 实际 %s", resourceID, event.ResourceID)
		}
		if event.NodeID != nodeID {
			t.Errorf("节点ID不匹配: 期望 %s, 实际 %s", nodeID, event.NodeID)
		}
		if !event.Success {
			t.Error("操作应该成功，但事件显示失败")
		}
		t.Logf("订阅者成功收到事件: %+v", event)
	}
}

// TestMultipleSubscribers 测试多个订阅者
func TestMultipleSubscribers(t *testing.T) {
	lm := NewLockManager(true)
	handler := NewHandler(lm)

	muxRouter := mux.NewRouter()
	handler.RegisterRoutes(muxRouter)
	router := httptest.NewServer(muxRouter)
	defer router.Close()

	resourceID := "sha256:test456"
	lockType := OperationTypePull

	// 创建多个订阅者
	subscriberCount := 3
	receivedEvents := make([][]OperationEvent, subscriberCount)
	subscriberDone := make([]chan bool, subscriberCount)

	for i := 0; i < subscriberCount; i++ {
		subscriberDone[i] = make(chan bool)
		receivedEvents[i] = make([]OperationEvent, 0)

		subscribeURL := router.URL + "/lock/subscribe?type=" + lockType + "&resource_id=" + resourceID
		req, _ := http.NewRequest("GET", subscribeURL, nil)
		recorder := httptest.NewRecorder()

		go func(idx int) {
			defer close(subscriberDone[idx])
			handler.Subscribe(recorder, req)

			body := recorder.Body.String()
			lines := strings.Split(body, "\n")
			for _, line := range lines {
				if strings.HasPrefix(line, "data: ") {
					eventJSON := strings.TrimPrefix(line, "data: ")
					var event OperationEvent
					if err := json.Unmarshal([]byte(eventJSON), &event); err == nil {
						receivedEvents[idx] = append(receivedEvents[idx], event)
					}
				}
			}
		}(i)
	}

	// 等待所有订阅者注册完成
	time.Sleep(200 * time.Millisecond)

	// 执行操作
	nodeID := "node-2"
	lockReq := &LockRequest{
		Type:       lockType,
		ResourceID: resourceID,
		NodeID:     nodeID,
	}
	acquired, _, _ := lm.TryLock(lockReq)
	if !acquired {
		t.Fatalf("无法获取锁")
	}

	time.Sleep(50 * time.Millisecond)

	unlockReq := &UnlockRequest{
		Type:       lockType,
		ResourceID: resourceID,
		NodeID:     nodeID,
		Error:      "", // 空字符串表示操作成功
	}
	lm.Unlock(unlockReq)

	// 等待事件广播
	time.Sleep(300 * time.Millisecond)

	// 验证所有订阅者都收到事件
	for i := 0; i < subscriberCount; i++ {
		if len(receivedEvents[i]) == 0 {
			t.Errorf("订阅者 %d 未收到事件", i)
		} else {
			event := receivedEvents[i][0]
			if event.ResourceID != resourceID || !event.Success {
				t.Errorf("订阅者 %d 收到的事件不正确: %+v", i, event)
			}
			t.Logf("订阅者 %d 收到事件: %+v", i, event)
		}
	}
}

// TestSubscriberHTTPEndpoint 测试 HTTP 订阅端点
func TestSubscriberHTTPEndpoint(t *testing.T) {
	lm := NewLockManager(true)
	handler := NewHandler(lm)

	muxRouter := mux.NewRouter()
	handler.RegisterRoutes(muxRouter)
	router := httptest.NewServer(muxRouter)
	defer router.Close()

	// 测试缺少参数的情况
	resp, err := http.Get(router.URL + "/lock/subscribe")
	if err != nil {
		t.Fatalf("请求失败: %v", err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("期望状态码 400, 实际 %d", resp.StatusCode)
	}

	// 测试缺少 resource_id 参数
	resp, err = http.Get(router.URL + "/lock/subscribe?type=pull")
	if err != nil {
		t.Fatalf("请求失败: %v", err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("期望状态码 400, 实际 %d", resp.StatusCode)
	}

	// 测试正确的订阅请求
	subscribeURL := router.URL + "/lock/subscribe?type=pull&resource_id=sha256:test789"
	req, err := http.NewRequest("GET", subscribeURL, nil)
	if err != nil {
		t.Fatalf("创建请求失败: %v", err)
	}

	recorder := httptest.NewRecorder()
	go handler.Subscribe(recorder, req)

	// 等待订阅建立
	time.Sleep(100 * time.Millisecond)

	// 验证响应头
	if recorder.Header().Get("Content-Type") != "text/event-stream" {
		t.Error("响应头 Content-Type 不正确")
	}
	if recorder.Header().Get("Cache-Control") != "no-cache" {
		t.Error("响应头 Cache-Control 不正确")
	}
}

// TestSubscriberUnsubscribe 测试取消订阅
func TestSubscriberUnsubscribe(t *testing.T) {
	lm := NewLockManager(true)

	resourceID := "sha256:test999"
	lockType := OperationTypePull

	// 创建模拟订阅者
	mockSubscriber := &mockSubscriber{
		events: make([]OperationEvent, 0),
		closed: false,
	}

	// 订阅
	lm.Subscribe(lockType, resourceID, mockSubscriber)

	// 取消订阅
	lm.Unsubscribe(lockType, resourceID, mockSubscriber)

	// 执行操作并释放锁
	lockReq := &LockRequest{
		Type:       lockType,
		ResourceID: resourceID,
		NodeID:     "node-test",
	}
	lm.TryLock(lockReq)

	unlockReq := &UnlockRequest{
		Type:       lockType,
		ResourceID: resourceID,
		NodeID:     "node-test",
		Error:      "", // 空字符串表示操作成功
	}
	lm.Unlock(unlockReq)

	// 等待广播
	time.Sleep(100 * time.Millisecond)

	// 验证取消订阅后不应该收到事件
	if len(mockSubscriber.events) > 0 {
		t.Error("取消订阅后仍收到事件")
	}
}

// mockSubscriber 模拟订阅者，用于测试
type mockSubscriber struct {
	events []OperationEvent
	closed bool
	mu     sync.Mutex
}

func (m *mockSubscriber) SendEvent(event *OperationEvent) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return fmt.Errorf("订阅者已关闭")
	}
	m.events = append(m.events, *event)
	return nil
}

func (m *mockSubscriber) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
}
