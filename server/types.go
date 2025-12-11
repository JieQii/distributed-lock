package server

import (
	"sync"
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
	Completed   bool         `json:"completed"`   // 操作是否完成
	Success     bool         `json:"success"`     // 操作是否成功
	CompletedAt time.Time    `json:"completed_at"` // 完成时间
}

// UnlockRequest 解锁请求
type UnlockRequest struct {
	Type       string `json:"type"`        // 操作类型：pull, update, delete
	ResourceID string `json:"resource_id"`
	NodeID     string `json:"node_id"`
	Success    bool   `json:"success"` // 操作是否成功
	Error      string `json:"error"`   // 错误信息
}

// ReferenceCount 引用计数信息（用于delete操作检查）
type ReferenceCount struct {
	Count    int            `json:"count"`     // 当前使用该资源的节点数
	Nodes    map[string]bool `json:"nodes"`   // 使用该资源的节点集合（用于调试和监控）
}

// LockKey 生成锁的唯一标识
func LockKey(lockType, resourceID string) string {
	return lockType + ":" + resourceID
}

