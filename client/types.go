package client

import (
	"context"
	"time"
)

// 操作类型常量（与服务端保持一致）
const (
	OperationTypePull   = "pull"   // 拉取镜像层
	OperationTypeUpdate = "update" // 更新镜像层
	OperationTypeDelete = "delete" // 删除镜像层
)

// Request 锁请求结构
// Type + ResourceID 作为仲裁Key作为唯一标识
type Request struct {
	Type       string `json:"type"`            // 仲裁类型：pull, update, delete
	ResourceID string `json:"resource_id"`     // 仲裁目标资源的唯一标识（镜像层的digest）
	NodeID     string `json:"node_id"`         // 发起仲裁的节点唯一标识
	Error      string `json:"error,omitempty"` // 错误信息（用于解锁时传递，序列化为字符串）
	// Success 字段已移除，服务端会根据 Error 自动推断：Error == "" → Success = true
	// contentv2 只需要设置 Error 即可
}

// LockResponse 加锁响应
type LockResponse struct {
	Acquired bool   `json:"acquired"` // 是否获得锁
	Skip     bool   `json:"skip"`     // 是否需要跳过操作（操作已完成且成功）
	Message  string `json:"message"`  // 响应消息
	Error    string `json:"error"`    // 错误信息（例如delete操作时引用计数不为0）
}

// UnlockResponse 解锁响应
type UnlockResponse struct {
	Released bool   `json:"released"` // 是否成功释放
	Message  string `json:"message"`  // 响应消息
}

// LockResult 加锁结果
type LockResult struct {
	Acquired bool  // 是否获得锁
	Skipped  bool  // 是否跳过操作（操作已完成且成功）
	Error    error // 错误信息
}

// OperationEvent 操作完成事件（与服务端保持一致）
type OperationEvent struct {
	Type        string    `json:"type"`         // 操作类型：pull, update, delete
	ResourceID  string    `json:"resource_id"`  // 资源ID
	NodeID      string    `json:"node_id"`      // 执行操作的节点ID
	Success     bool      `json:"success"`      // 操作是否成功
	Error       string    `json:"error"`        // 错误信息（如果有）
	CompletedAt time.Time `json:"completed_at"` // 完成时间
}

// ClusterLock 获取分布式锁
//  1. 获取到锁的直接返回，开始后续操作；
//  2. 没有获得锁的等待：
//     2.1. 获得锁的节点操作成功，那么直接返回，跳过下载操作；
//     2.2. 获得锁的节点操作失败，需要重新选择一个节点解锁，剩余节点继续等待；
func ClusterLock(ctx context.Context, client *LockClient, request *Request) (*LockResult, error) {
	return client.Lock(ctx, request)
}

// ClusterUnLock 释放分布式锁
// request需要携带处理结果以及错误信息
func ClusterUnLock(ctx context.Context, client *LockClient, request *Request) error {
	return client.Unlock(ctx, request)
}
