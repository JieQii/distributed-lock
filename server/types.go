package server

import (
	"time"
)

// 操作类型常量
const (
	OperationTypePull   = "pull"   // 拉取镜像层
	OperationTypeUpdate = "update" // 更新镜像层
	OperationTypeDelete = "delete" // 删除镜像层
)

// LockRequest 锁请求
type LockRequest struct {
	Type       string    `json:"type"`        // 操作类型：pull, update, delete
	ResourceID string    `json:"resource_id"` // 镜像层的digest
	NodeID     string    `json:"node_id"`
	Timestamp  time.Time // 请求时间戳，用于FIFO排序
	Error      string    `json:"error,omitempty"` // 错误信息（用于callback）
}

// LockInfo 锁信息
type LockInfo struct {
	Request     *LockRequest `json:"request"`
	AcquiredAt  time.Time    `json:"acquired_at"`
	Completed   bool         `json:"completed"`    // 操作是否完成
	Success     bool         `json:"success"`      // 操作是否成功
	CompletedAt time.Time    `json:"completed_at"` // 完成时间
}

// UnlockRequest 解锁请求
type UnlockRequest struct {
	Type       string `json:"type"` // 操作类型：pull, update, delete
	ResourceID string `json:"resource_id"`
	NodeID     string `json:"node_id"`
	Error      string `json:"error,omitempty"` // 错误信息（如果为空，表示操作成功）
	// Success 字段已移除，改为根据 Error 自动推断：Error == "" → Success = true
}

// 注意：ReferenceCount 类型已迁移到 callback 包
// 使用 callback.ReferenceCount 替代

// LockKey 生成锁的唯一标识
func LockKey(lockType, resourceID string) string {
	return lockType + ":" + resourceID
}

// OperationEvent 操作完成事件
type OperationEvent struct {
	Type        string    `json:"type"`         // 操作类型：pull, update, delete
	ResourceID  string    `json:"resource_id"`  // 资源ID
	NodeID      string    `json:"node_id"`      // 执行操作的节点ID
	Success     bool      `json:"success"`      // 操作是否成功
	Error       string    `json:"error"`        // 错误信息（如果有）
	CompletedAt time.Time `json:"completed_at"` // 完成时间
}

// Subscriber 订阅者接口
type Subscriber interface {
	// SendEvent 发送事件给订阅者
	SendEvent(event *OperationEvent) error
	// Close 关闭订阅者连接
	Close()
}
