package lockcallback

// 操作类型常量
const (
	OperationTypePull   = "pull"   // 拉取镜像层
	OperationTypeUpdate = "update" // 更新镜像层
	OperationTypeDelete = "delete" // 删除镜像层
)

// ReferenceCount 引用计数信息（用于delete操作检查）
type ReferenceCount struct {
	Count int             `json:"count"` // 当前使用该资源的节点数
	Nodes map[string]bool `json:"nodes"` // 使用该资源的节点集合（用于调试和监控）
}

// RefCountStorage 引用计数存储接口
// 实现此接口的类型需要提供引用计数的存储和获取功能
type RefCountStorage interface {
	// GetRefCount 获取资源的引用计数
	// 如果不存在，应该创建并初始化为0
	GetRefCount(resourceID string) *ReferenceCount

	// SetRefCount 设置资源的引用计数
	SetRefCount(resourceID string, refCount *ReferenceCount)

	// DeleteRefCount 删除资源的引用计数
	DeleteRefCount(resourceID string)
}

// OperationResult 操作结果
type OperationResult struct {
	Success bool   // 操作是否成功
	NodeID  string // 节点ID
	Error   error  // 错误信息（如果有）
}


