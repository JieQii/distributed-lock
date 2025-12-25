package client

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// LockClient 分布式锁客户端
type LockClient struct {
	ServerURL   string       // 锁服务端地址
	ShortClient *http.Client // 短连接客户端（用于普通HTTP请求，有超时）
	LongClient  *http.Client // 长连接客户端（用于SSE订阅和镜像操作，无超时）
	NodeID      string       // 当前节点ID

	// 重试配置
	MaxRetries     int           // 最大重试次数（默认3次）
	RetryInterval  time.Duration // 重试间隔（默认1秒）
	RequestTimeout time.Duration // 请求超时时间（默认30秒）
}

// NewLockClient 创建新的锁客户端
func NewLockClient(serverURL, nodeID string) *LockClient {
	return &LockClient{
		ServerURL: serverURL,
		ShortClient: &http.Client{
			Timeout: 30 * time.Second, // 短连接设置超时
		},
		LongClient: &http.Client{
			// 长连接不设置超时，用于SSE订阅和镜像操作（下载时间可能很长）
		},
		NodeID:         nodeID,
		MaxRetries:     3,
		RetryInterval:  1 * time.Second,
		RequestTimeout: 30 * time.Second,
	}
}

// Lock 获取锁（带重试机制）
func (c *LockClient) Lock(ctx context.Context, request *Request) (*LockResult, error) {
	// 设置节点ID
	request.NodeID = c.NodeID

	var lastErr error
	for attempt := 0; attempt <= c.MaxRetries; attempt++ {
		if attempt > 0 {
			// 重试前等待
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(c.RetryInterval):
			}
		}

		result, err := c.tryLockOnce(ctx, request)
		if err == nil {
			return result, nil
		}

		// 判断是否应该重试
		lastErr = err
		if !c.shouldRetry(err) {
			return nil, err
		}
	}

	return nil, fmt.Errorf("获取锁失败，已重试%d次: %w", c.MaxRetries, lastErr)
}

// tryLockOnce 尝试获取锁（单次尝试）
func (c *LockClient) tryLockOnce(ctx context.Context, request *Request) (*LockResult, error) {
	// 序列化请求
	jsonData, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("序列化请求失败: %w", err)
	}

	// 创建HTTP请求（使用传入的context，可以响应上层取消）
	// 超时由 ShortClient.Timeout 控制
	req, err := http.NewRequestWithContext(ctx, "POST", c.ServerURL+"/lock", bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	// 发送请求并等待响应（使用短连接客户端，有超时）
	resp, err := c.ShortClient.Do(req)
	if err != nil {
		// 注意：ShortClient.Timeout 触发时，err 会是 url.Error{Op: "Get", Err: context.DeadlineExceeded}
		// 所以我们直接判断 err 即可，不需要手动 WithTimeout
		if errors.Is(err, context.DeadlineExceeded) {
			return nil, fmt.Errorf("请求超时: %w", err)
		}
		if errors.Is(err, context.Canceled) {
			return nil, fmt.Errorf("请求被取消: %w", err)
		}
		return nil, fmt.Errorf("发送请求失败: %w", err)
	}
	defer resp.Body.Close()

	// 读取响应
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取响应失败: %w", err)
	}

	// 检查 HTTP 状态码，只有在成功时才解析 JSON
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusForbidden {
		// 返回错误，让上层 Lock 方法的重试机制处理
		return nil, fmt.Errorf("服务器返回错误状态码: %d, 响应: %s", resp.StatusCode, string(body))
	}

	// 解析响应
	var lockResp LockResponse
	if err := json.Unmarshal(body, &lockResp); err != nil {
		return nil, fmt.Errorf("解析响应失败: %w", err)
	}

	// 检查是否有错误（例如delete操作时引用计数不为0）
	if lockResp.Error != "" {
		return &LockResult{
			Acquired: false,
			Error:    fmt.Errorf("%s", lockResp.Error),
		}, nil
	}

	// 如果获得锁，直接返回
	if lockResp.Acquired {
		return &LockResult{
			Acquired: true,
		}, nil
	}

	// 如果没有获得锁，需要等待
	// 这里使用 SSE 订阅方式等待锁释放（不是轮询）
	// 注意：SSE订阅需要长时间保持连接，不应该使用带超时的context
	// 创建一个新的context，取消超时限制，但保留取消功能
	waitCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	return c.waitForLock(waitCtx, request)
}

// waitForLock 等待锁释放（使用 SSE 订阅模式）
func (c *LockClient) waitForLock(ctx context.Context, request *Request) (*LockResult, error) {
	// 定期检查锁是否已经被分配（用于处理操作失败的情况）
	// 如果操作失败，锁会被processQueue分配给队头节点，但不会广播事件
	// 队头节点需要定期重新请求锁来发现锁已被分配
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

resubscribe:
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
			// 定期重新请求锁，检查锁是否已经被processQueue分配
			// 这样可以处理操作失败的情况：锁被分配给队头节点，但不广播事件
			result, err := c.tryLockOnce(ctx, request)
			if err == nil && result.Acquired {
				return result, nil
			}
		default:
		}

		// 构建订阅 URL
		subscribeURL := fmt.Sprintf("%s/lock/subscribe?type=%s&resource_id=%s",
			c.ServerURL,
			url.QueryEscape(request.Type),
			url.QueryEscape(request.ResourceID))

		// 创建 SSE 订阅请求
		req, err := http.NewRequestWithContext(ctx, "GET", subscribeURL, nil)
		if err != nil {
			return nil, fmt.Errorf("创建订阅请求失败: %w", err)
		}
		req.Header.Set("Accept", "text/event-stream")
		req.Header.Set("Cache-Control", "no-cache")

		// 发送请求（使用长连接客户端，无超时限制）
		// 注意：SSE订阅需要长时间保持连接，用于镜像操作时下载时间可能很长
		resp, err := c.LongClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("订阅失败: %w", err)
		}

		// 检查响应状态码
		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			return nil, fmt.Errorf("订阅失败，状态码: %d, 响应: %s", resp.StatusCode, string(body))
		}

		// 使用 bufio.Scanner 读取 SSE 流
		scanner := bufio.NewScanner(resp.Body)
		var currentEventJSON string

		for scanner.Scan() {
			select {
			case <-ctx.Done():
				resp.Body.Close()
				return nil, ctx.Err()
			case <-ticker.C:
				// 定期重新请求锁，检查锁是否已经被processQueue分配
				// 这样可以处理操作失败的情况：锁被分配给队头节点，但不广播事件
				resp.Body.Close()
				result, err := c.tryLockOnce(ctx, request)
				if err == nil && result.Acquired {
					return result, nil
				}
				// 如果没有获得锁，继续SSE订阅（重新建立连接）
				goto resubscribe
			default:
			}

			line := scanner.Text()

			// SSE 格式: data: {json}\n\n
			// 空行表示事件结束
			if line == "" {
				// 处理之前收集的事件数据
				if currentEventJSON != "" {
					var event OperationEvent
					if err := json.Unmarshal([]byte(currentEventJSON), &event); err == nil {
						result, done, needResubscribe := c.handleOperationEvent(ctx, request, &event)
						if done {
							resp.Body.Close()
							return result, nil
						}
						if needResubscribe {
							// 需要重新订阅，关闭当前连接并重新开始
							resp.Body.Close()
							break
						}
					}
					currentEventJSON = ""
				}
			} else if strings.HasPrefix(line, "data: ") {
				// 提取 JSON 数据
				currentEventJSON = strings.TrimPrefix(line, "data: ")
			}
		}

		// 如果扫描结束（连接断开），检查是否有错误
		if err := scanner.Err(); err != nil {
			resp.Body.Close()
			return nil, fmt.Errorf("读取 SSE 流失败: %w", err)
		}

		// 处理最后一个事件（如果连接关闭前没有空行）
		if currentEventJSON != "" {
			var event OperationEvent
			if err := json.Unmarshal([]byte(currentEventJSON), &event); err == nil {
				result, done, needResubscribe := c.handleOperationEvent(ctx, request, &event)
				if done {
					resp.Body.Close()
					return result, nil
				}
				if needResubscribe {
					resp.Body.Close()
					// 继续外层循环，重新订阅
					continue
				}
			}
		}

		resp.Body.Close()

		// 连接正常关闭，但没有收到事件
		// 可能是操作失败，锁已经被processQueue分配给了队头节点
		// 重新请求锁，检查锁是否已经被分配
		result, err := c.tryLockOnce(ctx, request)
		if err == nil && result.Acquired {
			return result, nil
		}

		// 如果还是没有获得锁，继续等待（重新订阅）
		// 连接正常关闭，但没有收到事件，返回错误
		return nil, fmt.Errorf("SSE 连接关闭，未收到事件")
	}
}

// handleOperationEvent 处理操作完成事件
// 返回值: (结果, 是否完成, 是否需要重新订阅)
func (c *LockClient) handleOperationEvent(ctx context.Context, request *Request, event *OperationEvent) (*LockResult, bool, bool) {
	// 验证事件是否匹配当前请求
	if event.Type != request.Type || event.ResourceID != request.ResourceID {
		// 事件不匹配，继续等待
		return nil, false, false
	}

	// 检查是否有错误
	if event.Error != "" {
		return &LockResult{
			Acquired: false,
			Error:    fmt.Errorf("%s", event.Error),
		}, true, false
	}

	// 如果操作成功，说明其他节点已完成操作
	// 注意：上层已经检查过资源是否存在，如果资源已存在就不会请求锁
	// 所以这里不需要返回 Skipped，直接返回错误让上层处理
	// 上层应该通过文件系统检查资源是否已存在
	if event.Success {
		// 操作成功，但当前节点没有获得锁
		// 上层应该检查资源是否已存在，如果存在就不需要操作
		return &LockResult{
			Acquired: false,
			Error:    fmt.Errorf("其他节点已完成操作，请检查资源是否已存在"),
		}, true, false
	}

	// 如果操作失败，检查是否是"锁已分配"事件
	// 说明：当获得锁的节点操作失败时，服务端会：
	// 1. 删除锁
	// 2. 通过 processQueue 将锁分配给等待队列中的第一个节点（FIFO）
	// 3. 通过 notifyLockAssigned 发送事件通知队头节点锁已被分配
	//
	// 如果事件的NodeID匹配当前节点，说明锁已被分配给自己，应该立即重新请求锁
	// 如果事件的NodeID不匹配当前节点，说明锁被分配给了其他节点，需要继续等待
	if event.NodeID == request.NodeID {
		// 锁已被分配给自己，立即重新请求锁
		// 不需要等待，因为服务端已经完成了锁的分配
	} else {
		// 锁被分配给了其他节点，继续等待
		return nil, false, true // 重新订阅，继续等待
	}

	jsonData, err := json.Marshal(request)
	if err != nil {
		return &LockResult{
			Acquired: false,
			Error:    fmt.Errorf("序列化请求失败: %w", err),
		}, true, false
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.ServerURL+"/lock", bytes.NewBuffer(jsonData))
	if err != nil {
		return &LockResult{
			Acquired: false,
			Error:    fmt.Errorf("创建请求失败: %w", err),
		}, true, false
	}
	req.Header.Set("Content-Type", "application/json")

	// 使用短连接客户端（有超时）
	resp, err := c.ShortClient.Do(req)
	if err != nil {
		return &LockResult{
			Acquired: false,
			Error:    fmt.Errorf("获取锁失败: %w", err),
		}, true, false
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return &LockResult{
			Acquired: false,
			Error:    fmt.Errorf("读取响应失败: %w", err),
		}, true, false
	}

	var lockResp LockResponse
	if err := json.Unmarshal(body, &lockResp); err != nil {
		return &LockResult{
			Acquired: false,
			Error:    fmt.Errorf("解析响应失败: %w", err),
		}, true, false
	}

	// 检查是否有错误
	if lockResp.Error != "" {
		return &LockResult{
			Acquired: false,
			Error:    fmt.Errorf("%s", lockResp.Error),
		}, true, false
	}

	// 如果获得锁
	if lockResp.Acquired {
		return &LockResult{
			Acquired: true,
		}, true, false
	}

	// 没有获得锁，可能的原因：
	// 1. 锁还没有被 processQueue 分配（时序问题）
	// 2. 锁已经被队列中的其他节点获取
	// 3. 锁已经被其他节点获取
	//
	// 返回需要重新订阅，让节点重新请求锁
	// 此时锁应该已经被 processQueue 分配了（如果当前节点是队列中的第一个）
	// 或者需要继续等待（如果当前节点不是队列中的第一个）
	return nil, false, true
}

// shouldRetry 判断是否应该重试
func (c *LockClient) shouldRetry(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	// 网络错误、超时、连接失败等可以重试
	return contains(errStr, "timeout") ||
		contains(errStr, "connection") ||
		contains(errStr, "network") ||
		contains(errStr, "EOF") ||
		contains(errStr, "refused")
}

func contains(s, substr string) bool {
	if len(substr) == 0 {
		return true
	}
	if len(s) < len(substr) {
		return false
	}
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// Unlock 释放锁（带重试机制）
func (c *LockClient) Unlock(ctx context.Context, request *Request) error {
	// 设置节点ID
	request.NodeID = c.NodeID

	var lastErr error
	for attempt := 0; attempt <= c.MaxRetries; attempt++ {
		if attempt > 0 {
			// 重试前等待
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(c.RetryInterval):
			}
		}

		err := c.tryUnlockOnce(ctx, request)
		if err == nil {
			return nil
		}

		// 判断是否应该重试
		lastErr = err
		if !c.shouldRetry(err) {
			return err
		}
	}

	return fmt.Errorf("释放锁失败，已重试%d次: %w", c.MaxRetries, lastErr)
}

// tryUnlockOnce 尝试释放锁（单次尝试）
func (c *LockClient) tryUnlockOnce(ctx context.Context, request *Request) error {
	// Success 字段已移除，服务端会根据 Error 自动推断：Error == "" → Success = true
	// contentv2 只需要设置 Error 即可

	// 序列化请求
	jsonData, err := json.Marshal(request)
	if err != nil {
		return fmt.Errorf("序列化请求失败: %w", err)
	}

	// 创建HTTP请求（使用传入的context，可以响应上层取消）
	// 超时由 ShortClient.Timeout 控制
	req, err := http.NewRequestWithContext(ctx, "POST", c.ServerURL+"/unlock", bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("创建请求失败: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	// 发送请求（使用短连接客户端，有超时）
	resp, err := c.ShortClient.Do(req)
	if err != nil {
		// 注意：ShortClient.Timeout 触发时，err 会是 url.Error{Op: "Get", Err: context.DeadlineExceeded}
		// 所以我们直接判断 err 即可，不需要手动 WithTimeout
		if errors.Is(err, context.DeadlineExceeded) {
			return fmt.Errorf("请求超时: %w", err)
		}
		if errors.Is(err, context.Canceled) {
			return fmt.Errorf("请求被取消: %w", err)
		}
		return fmt.Errorf("发送请求失败: %w", err)
	}
	defer resp.Body.Close()

	// 读取响应
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("读取响应失败: %w", err)
	}

	// 解析响应
	var unlockResp UnlockResponse
	if err := json.Unmarshal(body, &unlockResp); err != nil {
		return fmt.Errorf("解析响应失败: %w", err)
	}

	if !unlockResp.Released {
		return fmt.Errorf("释放锁失败: %s", unlockResp.Message)
	}

	return nil
}
