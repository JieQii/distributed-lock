package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// LockClient 分布式锁客户端
type LockClient struct {
	ServerURL string       // 锁服务端地址
	Client    *http.Client // HTTP客户端
	NodeID    string       // 当前节点ID

	// 重试配置
	MaxRetries     int           // 最大重试次数（默认3次）
	RetryInterval  time.Duration // 重试间隔（默认1秒）
	RequestTimeout time.Duration // 请求超时时间（默认30秒）
}

// NewLockClient 创建新的锁客户端
func NewLockClient(serverURL, nodeID string) *LockClient {
	return &LockClient{
		ServerURL: serverURL,
		Client: &http.Client{
			Timeout: 30 * time.Second,
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

	// 创建带超时的context
	reqCtx, cancel := context.WithTimeout(ctx, c.RequestTimeout)
	defer cancel()

	// 创建HTTP请求
	req, err := http.NewRequestWithContext(reqCtx, "POST", c.ServerURL+"/lock", bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	// 发送请求并等待响应
	resp, err := c.Client.Do(req)
	if err != nil {
		// 检查是否是超时或连接错误
		if reqCtx.Err() == context.DeadlineExceeded {
			return nil, fmt.Errorf("请求超时: %w", err)
		}
		if reqCtx.Err() == context.Canceled {
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
			Skipped:  false,
			Error:    fmt.Errorf("%s", lockResp.Error),
		}, nil
	}

	// 如果需要跳过操作（操作已完成且成功），直接返回
	if lockResp.Skip {
		return &LockResult{
			Acquired: false,
			Skipped:  true,
		}, nil
	}

	// 如果获得锁，直接返回
	if lockResp.Acquired {
		return &LockResult{
			Acquired: true,
			Skipped:  false,
		}, nil
	}

	// 如果没有获得锁，需要等待
	// 这里使用轮询方式等待锁释放
	return c.waitForLock(ctx, request)
}

// waitForLock 等待锁释放
func (c *LockClient) waitForLock(ctx context.Context, request *Request) (*LockResult, error) {
	ticker := time.NewTicker(500 * time.Millisecond) // 每500ms轮询一次
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
			// 再次尝试获取锁
			jsonData, err := json.Marshal(request)
			if err != nil {
				continue
			}

			req, err := http.NewRequestWithContext(ctx, "POST", c.ServerURL+"/lock/status", bytes.NewBuffer(jsonData))
			if err != nil {
				continue
			}
			req.Header.Set("Content-Type", "application/json")

			resp, err := c.Client.Do(req)
			if err != nil {
				continue
			}

			body, err := io.ReadAll(resp.Body)
			resp.Body.Close()

			if err != nil {
				continue
			}

			var statusResp struct {
				Acquired  bool   `json:"acquired"`
				Completed bool   `json:"completed"` // 操作是否完成
				Success   bool   `json:"success"`   // 操作是否成功
				Error     string `json:"error"`     // 错误信息
			}
			if err := json.Unmarshal(body, &statusResp); err != nil {
				continue
			}

			// 检查是否有错误（例如delete操作时引用计数不为0）
			if statusResp.Error != "" {
				return &LockResult{
					Acquired: false,
					Skipped:  false,
					Error:    fmt.Errorf("%s", statusResp.Error),
				}, nil
			}

			// 如果操作已完成且成功，说明其他节点已经完成，当前节点跳过操作
			if statusResp.Completed && statusResp.Success {
				return &LockResult{
					Acquired: false,
					Skipped:  true,
				}, nil // 跳过下载操作
			}

			// 如果操作已完成但失败，继续等待获取锁
			if statusResp.Completed && !statusResp.Success {
				// 再次尝试获取锁
				jsonData, _ := json.Marshal(request)
				req, _ := http.NewRequestWithContext(ctx, "POST", c.ServerURL+"/lock", bytes.NewBuffer(jsonData))
				req.Header.Set("Content-Type", "application/json")
				resp, err := c.Client.Do(req)
				if err == nil {
					body, _ := io.ReadAll(resp.Body)
					resp.Body.Close()
					var lockResp LockResponse
					if json.Unmarshal(body, &lockResp) == nil {
						// 检查是否有错误
						if lockResp.Error != "" {
							return &LockResult{
								Acquired: false,
								Skipped:  false,
								Error:    fmt.Errorf("%s", lockResp.Error),
							}, nil
						}
						if lockResp.Skip {
							return &LockResult{
								Acquired: false,
								Skipped:  true,
							}, nil
						}
						if lockResp.Acquired {
							return &LockResult{
								Acquired: true,
								Skipped:  false,
							}, nil // 获得锁，可以开始操作
						}
					}
				}
			}

			// 如果获得锁，可以开始操作
			if statusResp.Acquired {
				return &LockResult{
					Acquired: true,
					Skipped:  false,
				}, nil
			}
		}
	}
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
	// 将 Err 转换为 Error 字符串（如果存在）
	if request.Err != nil && request.Error == "" {
		request.Error = request.Err.Error()
	}

	// 序列化请求
	jsonData, err := json.Marshal(request)
	if err != nil {
		return fmt.Errorf("序列化请求失败: %w", err)
	}

	// 创建带超时的context
	reqCtx, cancel := context.WithTimeout(ctx, c.RequestTimeout)
	defer cancel()

	// 创建HTTP请求
	req, err := http.NewRequestWithContext(reqCtx, "POST", c.ServerURL+"/unlock", bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("创建请求失败: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	// 发送请求
	resp, err := c.Client.Do(req)
	if err != nil {
		// 检查是否是超时或连接错误
		if reqCtx.Err() == context.DeadlineExceeded {
			return fmt.Errorf("请求超时: %w", err)
		}
		if reqCtx.Err() == context.Canceled {
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
