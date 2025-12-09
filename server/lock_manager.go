package server

import (
	"hash/fnv"
	"sync"
	"time"
)

const (
	// shardCount 分段锁的数量，建议使用2的幂次方
	// 可以根据实际并发需求调整，例如：16, 32, 64, 128
	shardCount = 32
)

// resourceShard 资源分段，每个分段有自己的锁和数据结构
type resourceShard struct {
	mu sync.RWMutex

	// 当前持有的锁：resourceID -> LockInfo
	locks map[string]*LockInfo

	// 等待队列：resourceID -> []*LockRequest (FIFO队列)
	queues map[string][]*LockRequest

	// 引用计数：resourceID -> ReferenceCount
	// 记录使用该资源的节点数和节点集合（用于delete操作检查）
	refCounts map[string]*ReferenceCount
}

// LockManager 锁管理器
// 使用分段锁提升并发度：不同资源可以并发访问，只有相同分段的资源才会竞争
type LockManager struct {
	shards [shardCount]*resourceShard
	
	// UpdateRequiresNoRef 配置：update操作是否需要引用计数为0
	// true: update操作必须引用计数为0才能执行（不允许热更新）
	// false: update操作允许在有引用时执行（允许热更新，默认）
	UpdateRequiresNoRef bool
}

// getShard 根据key获取对应的分段
func (lm *LockManager) getShard(key string) *resourceShard {
	// 使用FNV-1a哈希算法计算分段索引
	h := fnv.New32a()
	h.Write([]byte(key))
	return lm.shards[h.Sum32()%shardCount]
}

// NewLockManager 创建新的锁管理器
func NewLockManager() *LockManager {
	lm := &LockManager{
		UpdateRequiresNoRef: false, // 默认允许热更新
	}
	// 初始化所有分段
	for i := 0; i < shardCount; i++ {
		lm.shards[i] = &resourceShard{
			locks:     make(map[string]*LockInfo),
			queues:    make(map[string][]*LockRequest),
			refCounts: make(map[string]*ReferenceCount),
		}
	}
	return lm
}

// TryLock 尝试获取锁
// 仲裁逻辑：
// 1. 检查是否有其他节点在操作（锁是否被占用）
// 2. 检查引用计数是否符合预期（判断操作是否已完成但还没刷新mergerfs）
//    - Pull: 预期refcount != 0 时跳过（已下载完成）
//    - Delete: 预期refcount == 0 时跳过（已删除完成）
//    - Update: 根据配置决定
// 返回：是否获得锁，是否操作已完成且成功（需要跳过操作），错误信息
func (lm *LockManager) TryLock(request *LockRequest) (bool, bool, string) {
	key := LockKey(request.Type, request.ResourceID)
	shard := lm.getShard(key) // 获取对应的分段

	shard.mu.Lock()
	defer shard.mu.Unlock()

	request.Timestamp = time.Now()

	// 获取引用计数
	refCount := lm.getRefCount(shard, request.ResourceID)

	// 第一步：检查是否已经有锁（是否有其他节点在操作）
	if lockInfo, exists := shard.locks[key]; exists {
		// 如果操作已完成且成功，返回true表示需要跳过操作
		if lockInfo.Completed && lockInfo.Success {
			return false, true, ""
		}

		// 如果操作已完成但失败，释放锁并继续处理队列
		if lockInfo.Completed && !lockInfo.Success {
			delete(shard.locks, key)
			// 继续处理队列中的下一个请求
			lm.processQueue(shard, key)
		} else {
			// 锁被占用但操作未完成，加入等待队列
			lm.addToQueue(shard, key, request)
			return false, false, ""
		}
	}

	// 第二步：检查引用计数是否符合预期（判断操作是否已完成但还没刷新mergerfs）
	// 这个检查在锁不存在时进行，用于判断是否应该跳过操作
	switch request.Type {
	case OperationTypePull:
		// Pull逻辑：如果refcount != 0，说明已经下载完成（但还没刷新mergerfs），应该跳过
		if refCount.Count > 0 {
			// 引用计数不为0，说明已经下载完成，跳过操作
			return false, true, ""
		}
		// 引用计数为0，可以继续执行pull操作

	case OperationTypeDelete:
		// Delete逻辑：
		// 1. 如果refcount > 0，不能执行delete操作（有节点在使用）
		if refCount.Count > 0 {
			return false, false, "无法删除：当前有节点正在使用该资源"
		}
		// 2. 如果refcount == 0，说明已经删除完成（但还没刷新mergerfs），应该跳过
		// 注意：这里refcount == 0可能是两种情况：
		// - 资源从未被pull过（refcount初始为0）
		// - 资源已经被删除完成（但还没刷新mergerfs）
		// 为了区分这两种情况，我们需要检查是否有已完成且成功的delete操作
		// 但由于锁不存在，说明没有正在进行的delete操作
		// 如果refcount == 0且没有锁，可能是资源不存在，可以尝试删除
		// 但为了安全，我们仍然允许获取锁执行delete操作
		// 如果资源已经删除，delete操作会失败，这是合理的

	case OperationTypeUpdate:
		// Update逻辑：
		// 1. 如果配置要求UpdateRequiresNoRef且refcount > 0，不能执行update操作
		if lm.UpdateRequiresNoRef && refCount.Count > 0 {
			return false, false, "无法更新：当前有节点正在使用该资源，不允许更新"
		}
		// 2. 如果refcount > 0，说明资源存在且在使用中，可以执行update（热更新）
		// 3. 如果refcount == 0，说明资源不存在或已删除，可以执行update（创建或更新）
		// Update操作不检查refcount来决定是否跳过，因为update可能用于创建新资源
	}

	// 没有锁且引用计数检查通过，直接获取锁
	shard.locks[key] = &LockInfo{
		Request:    request,
		AcquiredAt: time.Now(),
		Completed:  false,
		Success:    false,
	}

	return true, false, ""
}

// Unlock 释放锁
func (lm *LockManager) Unlock(request *UnlockRequest) bool {
	key := LockKey(request.Type, request.ResourceID)
	shard := lm.getShard(key) // 获取对应的分段

	shard.mu.Lock()
	defer shard.mu.Unlock()

	// 检查锁是否存在
	lockInfo, exists := shard.locks[key]
	if !exists {
		return false
	}

	// 检查是否是锁的持有者
	if lockInfo.Request.NodeID != request.NodeID {
		return false
	}

	// 更新锁信息
	lockInfo.Completed = true
	lockInfo.Success = request.Success
	lockInfo.CompletedAt = time.Now()

	// 根据操作类型更新引用计数（仅在操作成功时）
	if request.Success {
		lm.updateRefCount(shard, request.Type, request.ResourceID, request.NodeID, true)
	}

	// 如果操作成功，保留锁信息一段时间（用于其他节点查询状态）
	// 如果操作失败，立即释放锁并处理队列
	if request.Success {
		// 操作成功，保留锁信息，但标记为已完成
		// 其他节点查询时会发现已完成且成功，跳过操作
		go func() {
			time.Sleep(5 * time.Minute) // 5分钟后清理
			shard.mu.Lock()
			defer shard.mu.Unlock()
			if lock, exists := shard.locks[key]; exists && lock.Completed && lock.Success {
				delete(shard.locks, key)
			}
		}()
	} else {
		// 操作失败，立即释放锁
		delete(shard.locks, key)
		// 处理队列中的下一个请求
		lm.processQueue(shard, key)
	}

	return true
}

// GetLockStatus 获取锁状态
// 返回：是否是当前节点持有的锁，操作是否完成，操作是否成功
func (lm *LockManager) GetLockStatus(lockType, resourceID, nodeID string) (bool, bool, bool) {
	key := LockKey(lockType, resourceID)
	shard := lm.getShard(key) // 获取对应的分段

	shard.mu.RLock()
	defer shard.mu.RUnlock()

	lockInfo, exists := shard.locks[key]

	if !exists {
		return false, false, false // 没有锁，未完成，未成功
	}

	// 检查是否是当前节点持有的锁
	acquired := lockInfo.Request.NodeID == nodeID

	return acquired, lockInfo.Completed, lockInfo.Success
}

// addToQueue 添加请求到等待队列（FIFO）
// 注意：调用此函数时，shard.mu 必须已经加锁
func (lm *LockManager) addToQueue(shard *resourceShard, key string, request *LockRequest) {
	if _, exists := shard.queues[key]; !exists {
		shard.queues[key] = make([]*LockRequest, 0)
	}
	shard.queues[key] = append(shard.queues[key], request)
}

// processQueue 处理等待队列（FIFO）
// 注意：调用此函数时，shard.mu 必须已经加锁
func (lm *LockManager) processQueue(shard *resourceShard, key string) {
	queue, exists := shard.queues[key]
	if !exists || len(queue) == 0 {
		return
	}

	// FIFO：取出队列中的第一个请求
	nextRequest := queue[0]
	shard.queues[key] = queue[1:]

	// 如果队列为空，删除队列
	if len(shard.queues[key]) == 0 {
		delete(shard.queues, key)
	}

	// 分配锁给下一个请求
	shard.locks[key] = &LockInfo{
		Request:    nextRequest,
		AcquiredAt: time.Now(),
		Completed:  false,
		Success:    false,
	}
}

// GetQueueLength 获取队列长度（用于监控）
func (lm *LockManager) GetQueueLength(lockType, resourceID string) int {
	key := LockKey(lockType, resourceID)
	shard := lm.getShard(key)

	shard.mu.RLock()
	defer shard.mu.RUnlock()

	queue, exists := shard.queues[key]
	if !exists {
		return 0
	}
	return len(queue)
}

// GetLockInfo 获取锁信息（用于调试和监控）
func (lm *LockManager) GetLockInfo(lockType, resourceID string) *LockInfo {
	key := LockKey(lockType, resourceID)
	shard := lm.getShard(key)

	shard.mu.RLock()
	defer shard.mu.RUnlock()

	return shard.locks[key]
}

// getRefCount 获取资源的引用计数
// 注意：调用此函数时，shard.mu 必须已经加锁
func (lm *LockManager) getRefCount(shard *resourceShard, resourceID string) *ReferenceCount {
	refCount, exists := shard.refCounts[resourceID]
	if !exists {
		// 如果不存在，创建并初始化为0
		refCount = &ReferenceCount{
			Count: 0,
			Nodes: make(map[string]bool),
		}
		shard.refCounts[resourceID] = refCount
	}
	return refCount
}

// updateRefCount 更新引用计数
// 注意：调用此函数时，shard.mu 必须已经加锁
func (lm *LockManager) updateRefCount(shard *resourceShard, operationType, resourceID, nodeID string, increment bool) {
	refCount := lm.getRefCount(shard, resourceID)

	switch operationType {
	case OperationTypePull:
		// pull操作成功：引用计数+1（节点开始使用该资源）
		if increment {
			if !refCount.Nodes[nodeID] {
				refCount.Count++
				refCount.Nodes[nodeID] = true
			}
		} else {
			// pull操作失败：不改变引用计数
		}
	case OperationTypeUpdate:
		// update操作：不改变引用计数
	case OperationTypeDelete:
		// delete操作成功：引用计数应该已经是0（在TryLock时已检查）
		// delete成功后，资源被删除，但引用计数信息保留用于监控
		// 如果需要清理，可以在这里删除refCounts[resourceID]
		if increment {
			// delete成功，可以清理引用计数（因为资源已删除）
			delete(shard.refCounts, resourceID)
		}
	}
}

// GetRefCount 获取资源的引用计数（用于监控和调试）
func (lm *LockManager) GetRefCount(resourceID string) *ReferenceCount {
	// 使用resourceID作为key的一部分来获取分段
	// 注意：这里使用resourceID而不是完整的key，因为引用计数是按resourceID存储的
	shard := lm.getShard(resourceID)

	shard.mu.RLock()
	defer shard.mu.RUnlock()

	return lm.getRefCount(shard, resourceID)
}

// ReleaseNodeRefs 释放节点对资源的所有引用（用于节点断开连接时清理）
func (lm *LockManager) ReleaseNodeRefs(nodeID string) {
	// 遍历所有分段，清理该节点的引用
	for i := 0; i < shardCount; i++ {
		shard := lm.shards[i]
		shard.mu.Lock()

		// 遍历该分段的所有引用计数
		for resourceID, refCount := range shard.refCounts {
			if refCount.Nodes[nodeID] {
				refCount.Count--
				delete(refCount.Nodes, nodeID)
				// 如果引用计数为0，可以选择删除该条目
				if refCount.Count == 0 {
					delete(shard.refCounts, resourceID)
				}
			}
		}

		shard.mu.Unlock()
	}
}

