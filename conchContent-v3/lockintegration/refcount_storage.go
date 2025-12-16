package lockintegration

import (
	"sync"

	"conchContent-v3/lockcallback"
)

// RefCountStorage 本地计数存储接口，方便后续替换为文件/DB
type RefCountStorage interface {
	lockcallback.RefCountStorage
}

// LocalRefCountStorage 默认的本地内存实现
type LocalRefCountStorage struct {
	mu        sync.RWMutex
	refCounts map[string]*lockcallback.ReferenceCount
}

// NewLocalRefCountStorage 创建本地存储
func NewLocalRefCountStorage() *LocalRefCountStorage {
	return &LocalRefCountStorage{
		refCounts: make(map[string]*lockcallback.ReferenceCount),
	}
}

// GetRefCount 获取引用计数（若不存在则初始化）
func (s *LocalRefCountStorage) GetRefCount(resourceID string) *lockcallback.ReferenceCount {
	s.mu.RLock()
	ref, ok := s.refCounts[resourceID]
	s.mu.RUnlock()
	if !ok {
		return &lockcallback.ReferenceCount{
			Count: 0,
			Nodes: map[string]bool{},
		}
	}

	// 返回副本防止外部修改
	nodesCopy := make(map[string]bool)
	for k, v := range ref.Nodes {
		nodesCopy[k] = v
	}
	return &lockcallback.ReferenceCount{
		Count: ref.Count,
		Nodes: nodesCopy,
	}
}

// SetRefCount 写入引用计数
func (s *LocalRefCountStorage) SetRefCount(resourceID string, refCount *lockcallback.ReferenceCount) {
	nodesCopy := make(map[string]bool)
	for k, v := range refCount.Nodes {
		nodesCopy[k] = v
	}

	s.mu.Lock()
	s.refCounts[resourceID] = &lockcallback.ReferenceCount{
		Count: refCount.Count,
		Nodes: nodesCopy,
	}
	s.mu.Unlock()
}

// DeleteRefCount 删除引用计数
func (s *LocalRefCountStorage) DeleteRefCount(resourceID string) {
	s.mu.Lock()
	delete(s.refCounts, resourceID)
	s.mu.Unlock()
}


