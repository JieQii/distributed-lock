package content

import (
	"context"
	"fmt"

	"distributed-lock/callback"
)

// ExampleWithCallback 示例：Content插件直接使用callback包
func ExampleWithCallback() {
	_ = context.Background()
	resourceID := "sha256:test123"
	nodeID := "node-1"

	// 创建自定义存储（例如：基于本地文件或数据库）
	// 这里使用内存存储作为示例
	storage := &MemoryRefCountStorage{
		refCounts: make(map[string]*callback.ReferenceCount),
	}

	// 创建引用计数管理器
	manager := callback.NewRefCountManager(storage)

	// 判断是否应该跳过Pull操作
	skip, errMsg := manager.ShouldSkipOperation(callback.OperationTypePull, resourceID)
	if skip {
		fmt.Printf("应该跳过Pull操作: %s\n", errMsg)
		return
	}

	// 执行Pull操作
	// ... 实际的下载逻辑 ...

	// 操作成功后，更新引用计数
	result := &callback.OperationResult{
		Success: true,
		NodeID:  nodeID,
	}
	manager.UpdateRefCount(callback.OperationTypePull, resourceID, result)

	// 检查引用计数
	refCount := manager.GetRefCount(resourceID)
	fmt.Printf("引用计数: %d, 节点: %v\n", refCount.Count, refCount.Nodes)
}

// MemoryRefCountStorage 内存存储实现（示例）
type MemoryRefCountStorage struct {
	refCounts map[string]*callback.ReferenceCount
}

func (s *MemoryRefCountStorage) GetRefCount(resourceID string) *callback.ReferenceCount {
	refCount, exists := s.refCounts[resourceID]
	if !exists {
		refCount = &callback.ReferenceCount{
			Count: 0,
			Nodes: make(map[string]bool),
		}
		s.refCounts[resourceID] = refCount
	}
	// 返回副本，避免并发修改
	nodesCopy := make(map[string]bool)
	for k, v := range refCount.Nodes {
		nodesCopy[k] = v
	}
	return &callback.ReferenceCount{
		Count: refCount.Count,
		Nodes: nodesCopy,
	}
}

func (s *MemoryRefCountStorage) SetRefCount(resourceID string, refCount *callback.ReferenceCount) {
	// 保存副本，避免并发修改
	nodesCopy := make(map[string]bool)
	for k, v := range refCount.Nodes {
		nodesCopy[k] = v
	}
	s.refCounts[resourceID] = &callback.ReferenceCount{
		Count: refCount.Count,
		Nodes: nodesCopy,
	}
}

func (s *MemoryRefCountStorage) DeleteRefCount(resourceID string) {
	delete(s.refCounts, resourceID)
}
