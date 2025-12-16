package lockcallback

// RefCountManager 引用计数管理器
// 提供引用计数的更新、查询等功能
type RefCountManager struct {
	storage RefCountStorage
}

// NewRefCountManager 创建新的引用计数管理器
func NewRefCountManager(storage RefCountStorage) *RefCountManager {
	return &RefCountManager{
		storage: storage,
	}
}

// UpdateRefCount 更新引用计数
// operationType: 操作类型（pull, update, delete）
// resourceID: 资源ID
// result: 操作结果
func (m *RefCountManager) UpdateRefCount(operationType, resourceID string, result *OperationResult) {
	if !result.Success {
		// 操作失败，不更新引用计数
		return
	}

	switch operationType {
	case OperationTypePull:
		// pull操作成功：引用计数+1（节点开始使用该资源）
		refCount := m.storage.GetRefCount(resourceID)
		if !refCount.Nodes[result.NodeID] {
			// 创建新的引用计数对象，避免修改原对象
			newRefCount := &ReferenceCount{
				Count: refCount.Count + 1,
				Nodes: make(map[string]bool),
			}
			// 复制原有节点
			for k, v := range refCount.Nodes {
				newRefCount.Nodes[k] = v
			}
			// 添加新节点
			newRefCount.Nodes[result.NodeID] = true
			m.storage.SetRefCount(resourceID, newRefCount)
		}

	case OperationTypeUpdate:
		// update操作：不改变引用计数
		// 不需要更新

	case OperationTypeDelete:
		// delete操作成功：引用计数应该已经是0（在TryLock时已检查）
		// delete成功后，资源被删除，清理引用计数信息
		m.storage.DeleteRefCount(resourceID)
	}
}

// GetRefCount 获取资源的引用计数
func (m *RefCountManager) GetRefCount(resourceID string) *ReferenceCount {
	return m.storage.GetRefCount(resourceID)
}

// ReleaseNodeRefs 释放节点对资源的所有引用（用于节点断开连接时清理）
// nodeID: 节点ID
// 返回：释放的引用数量
func (m *RefCountManager) ReleaseNodeRefs(nodeID string) int {
	// 注意：这个方法需要遍历所有资源，但RefCountStorage接口没有提供遍历方法
	// 如果需要实现此功能，需要在接口中添加遍历方法
	// 或者由实现类提供专门的清理方法
	// 这里先提供一个基础实现，具体实现由存储层提供
	return 0
}

// ShouldSkipOperation 判断是否应该跳过操作
// operationType: 操作类型
// resourceID: 资源ID
// 返回：是否应该跳过，错误信息
func (m *RefCountManager) ShouldSkipOperation(operationType, resourceID string) (bool, string) {
	refCount := m.GetRefCount(resourceID)

	switch operationType {
	case OperationTypePull:
		// Pull逻辑：如果refcount != 0，说明已经下载完成（但还没刷新mergerfs），应该跳过
		if refCount.Count > 0 {
			return true, ""
		}

	case OperationTypeDelete:
		// Delete逻辑：
		// 1. 如果refcount > 0，不能执行delete操作（有节点在使用）
		if refCount.Count > 0 {
			return false, "无法删除：当前有节点正在使用该资源"
		}
		// 2. 如果refcount == 0，允许执行delete操作（可能资源不存在或已删除）

	case OperationTypeUpdate:
		// Update操作不基于refcount来决定是否跳过
		// 由配置决定是否允许在有引用时更新
	}

	return false, ""
}


