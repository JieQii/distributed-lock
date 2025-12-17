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
}

// LockManager 锁管理器
// 使用分段锁提升并发度：不同资源可以并发访问，只有相同分段的资源才会竞争
type LockManager struct {
	shards [shardCount]*resourceShard
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
	lm := &LockManager{}
	// 初始化所有分段
	for i := 0; i < shardCount; i++ {
		lm.shards[i] = &resourceShard{
			locks:  make(map[string]*LockInfo),
			queues: make(map[string][]*LockRequest),
		}
	}
	return lm
}

// TryLock 尝试获取锁
// 仲裁逻辑：
// 1. 检查是否有其他节点在操作（锁是否被占用）
// 2. 检查引用计数是否符合预期（判断操作是否已完成但还没刷新mergerfs）
//   - Pull: 预期refcount != 0 时跳过（已下载完成）
//   - Delete: 预期refcount == 0 时跳过（已删除完成）
//   - Update: 根据配置决定
//
// 返回：是否获得锁，是否操作已完成且成功（需要跳过操作），错误信息
func (lm *LockManager) TryLock(request *LockRequest) (bool, bool, string) {
	key := LockKey(request.Type, request.ResourceID)
	shard := lm.getShard(key) // 获取对应的分段

	shard.mu.Lock()
	defer shard.mu.Unlock()

	request.Timestamp = time.Now()

	// 第一步：检查是否已经有锁（是否有其他节点在操作）
	if lockInfo, exists := shard.locks[key]; exists {
		// 如果操作已完成，释放锁并继续处理队列
		if lockInfo.Completed {
			delete(shard.locks, key)
			// 继续处理队列中的下一个请求
			lm.processQueue(shard, key)
		} else {
			// 锁被占用但操作未完成
			// 如果当前请求的节点就是锁的持有者（可能是队列中的旧请求被分配了锁，现在客户端重新请求）
			if lockInfo.Request.NodeID == request.NodeID {
				// 注意：这里允许同一节点获取锁，但实际使用中，客户端应该在请求锁之前
				// 先检查引用计数（ShouldSkipOperation），如果资源已存在，不应该请求锁
				// 这个逻辑主要用于处理队列场景：队列中的旧请求被分配锁后，客户端通过轮询重新请求
				// 更新锁的请求信息（使用最新的请求）
				lockInfo.Request = request
				lockInfo.AcquiredAt = time.Now()
				return true, false, ""
			}
			// 其他节点持有锁，加入等待队列
			lm.addToQueue(shard, key, request)
			return false, false, ""
		}
	}

	// 没有锁，直接获取锁
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

	// 释放锁并处理队列
	delete(shard.locks, key)
	lm.processQueue(shard, key)

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

// 引用计数相关逻辑已移至 content 插件侧的 callback 使用中
