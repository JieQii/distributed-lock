package callback

import (
	"fmt"
)

// ExampleUsage 示例：如何使用callback包
func ExampleUsage() {
	// 创建自定义的存储实现
	storage := &ExampleStorage{
		refCounts: make(map[string]*ReferenceCount),
	}

	// 创建引用计数管理器
	manager := NewRefCountManager(storage)

	// 示例1：Pull操作成功，更新引用计数
	result1 := &OperationResult{
		Success: true,
		NodeID:  "node-1",
	}
	manager.UpdateRefCount(OperationTypePull, "sha256:test123", result1)

	// 检查引用计数
	refCount := manager.GetRefCount("sha256:test123")
	fmt.Printf("引用计数: %d, 节点: %v\n", refCount.Count, refCount.Nodes)

	// 示例2：判断是否应该跳过Pull操作
	skip, _ := manager.ShouldSkipOperation(OperationTypePull, "sha256:test123")
	if skip {
		fmt.Println("应该跳过Pull操作（引用计数 > 0）")
	}

	// 示例3：判断是否可以执行Delete操作
	canDelete, errMsg := manager.CanPerformOperation(OperationTypeDelete, "sha256:test123", false)
	if !canDelete {
		fmt.Printf("不能执行Delete操作: %s\n", errMsg)
	}
}

// ExampleStorage 示例存储实现
type ExampleStorage struct {
	refCounts map[string]*ReferenceCount
}

func (s *ExampleStorage) GetRefCount(resourceID string) *ReferenceCount {
	refCount, exists := s.refCounts[resourceID]
	if !exists {
		refCount = &ReferenceCount{
			Count: 0,
			Nodes: make(map[string]bool),
		}
		s.refCounts[resourceID] = refCount
	}
	return refCount
}

func (s *ExampleStorage) SetRefCount(resourceID string, refCount *ReferenceCount) {
	s.refCounts[resourceID] = refCount
}

func (s *ExampleStorage) DeleteRefCount(resourceID string) {
	delete(s.refCounts, resourceID)
}
