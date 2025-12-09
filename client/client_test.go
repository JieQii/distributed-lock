package client

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// TestConcurrentPullOperations 测试并发pull操作
func TestConcurrentPullOperations(t *testing.T) {
	// 创建模拟服务器
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/lock" {
			// 模拟锁服务
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"acquired":true,"skip":false,"message":"成功获得锁"}`))
		} else if r.URL.Path == "/unlock" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"released":true,"message":"成功释放锁"}`))
		}
	}))
	defer server.Close()

	client := NewLockClient(server.URL, "test-node")
	resourceID := "sha256:test123"

	// 并发执行10个pull操作
	var wg sync.WaitGroup
	successCount := 0
	var mu sync.Mutex
	concurrency := 10

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(nodeID string) {
			defer wg.Done()
			ctx := context.Background()
			request := &Request{
				Type:       OperationTypePull,
				ResourceID: resourceID,
				NodeID:     nodeID,
			}

			result, err := client.Lock(ctx, request)
			if err != nil {
				t.Logf("节点 %s 获取锁失败: %v", nodeID, err)
				return
			}

			if result.Acquired {
				mu.Lock()
				successCount++
				mu.Unlock()

				// 模拟pull操作
				time.Sleep(10 * time.Millisecond)

				// 释放锁
				request.Success = true
				if err := client.Unlock(ctx, request); err != nil {
					t.Logf("节点 %s 释放锁失败: %v", nodeID, err)
				}
			}
		}(fmt.Sprintf("node-%d", i))
	}

	wg.Wait()
	t.Logf("并发pull操作完成，成功获取锁的节点数: %d", successCount)
}

// TestReferenceCountConcurrency 测试引用计数的并发安全性
func TestReferenceCountConcurrency(t *testing.T) {
	// 这个测试需要真实的服务器，所以这里只是示例
	// 实际测试应该启动真实的服务器实例
	t.Skip("需要真实服务器实例")
}

// TestDeleteWithReferences 测试有引用时删除操作
func TestDeleteWithReferences(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/lock" {
			// 模拟delete操作时引用计数不为0的情况
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte(`{"acquired":false,"skip":false,"error":"无法删除：当前有节点正在使用该资源","message":"无法删除：当前有节点正在使用该资源"}`))
		}
	}))
	defer server.Close()

	client := NewLockClient(server.URL, "test-node")
	ctx := context.Background()
	request := &Request{
		Type:       OperationTypeDelete,
		ResourceID: "sha256:test123",
		NodeID:     "test-node",
	}

	result, err := client.Lock(ctx, request)
	if err != nil {
		t.Fatalf("获取锁时发生错误: %v", err)
	}

	if result.Error == nil {
		t.Error("期望返回错误，但没有错误")
	} else {
		t.Logf("正确返回错误: %v", result.Error)
	}
}

// TestRetryMechanism 测试重试机制
func TestRetryMechanism(t *testing.T) {
	attemptCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attemptCount++
		if attemptCount < 3 {
			// 前两次返回连接错误
			http.Error(w, "connection refused", http.StatusServiceUnavailable)
			return
		}
		// 第三次成功
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"acquired":true,"skip":false,"message":"成功获得锁"}`))
	}))
	defer server.Close()

	client := NewLockClient(server.URL, "test-node")
	client.MaxRetries = 3
	client.RetryInterval = 100 * time.Millisecond

	ctx := context.Background()
	request := &Request{
		Type:       OperationTypePull,
		ResourceID: "sha256:test123",
		NodeID:     "test-node",
	}

	result, err := client.Lock(ctx, request)
	if err != nil {
		t.Fatalf("获取锁失败: %v", err)
	}

	if !result.Acquired {
		t.Error("期望获得锁，但没有获得")
	}

	if attemptCount != 3 {
		t.Errorf("期望重试3次，实际重试次数: %d", attemptCount-1)
	}
}

// TestTimeout 测试超时处理
func TestTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 模拟慢响应
		time.Sleep(2 * time.Second)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"acquired":true,"skip":false,"message":"成功获得锁"}`))
	}))
	defer server.Close()

	client := NewLockClient(server.URL, "test-node")
	client.RequestTimeout = 500 * time.Millisecond

	ctx := context.Background()
	request := &Request{
		Type:       OperationTypePull,
		ResourceID: "sha256:test123",
		NodeID:     "test-node",
	}

	_, err := client.Lock(ctx, request)
	if err == nil {
		t.Error("期望超时错误，但没有错误")
	} else {
		t.Logf("正确返回超时错误: %v", err)
	}
}

