package server

import (
	"hash/fnv"
	"log"
	"strings"
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

	// 订阅者：resourceID -> []Subscriber
	subscribers map[string][]Subscriber
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
			locks:       make(map[string]*LockInfo),
			queues:      make(map[string][]*LockRequest),
			subscribers: make(map[string][]Subscriber),
		}
	}
	return lm
}

// TryLock 尝试获取锁
// 仲裁逻辑：
// 1. 检查是否有其他节点在操作（锁是否被占用）
// 2. 如果锁被占用，检查是否是同一节点重新请求（队列场景）
// 3. 如果锁未被占用，创建新的资源锁
//
// 注意：引用计数检查应该在客户端进行（ShouldSkipOperation），
// 服务端只负责锁的分配和队列管理，不检查引用计数。
// 客户端在请求锁之前应该先检查本地引用计数，如果资源已存在，不应该请求锁。
//
// 返回：是否获得锁，是否操作已完成且成功（需要跳过操作），错误信息
func (lm *LockManager) TryLock(request *LockRequest) (bool, bool, string) {
	key := LockKey(request.Type, request.ResourceID)
	shard := lm.getShard(key) // 获取对应的分段

	// 1. 获取分段锁
	shard.mu.Lock()

	request.Timestamp = time.Now()

	// 2. 检查资源锁是否存在
	if lockInfo, exists := shard.locks[key]; exists {
		// 资源锁存在
		// 如果操作已完成（这种情况不应该发生，因为操作成功时锁已被删除）
		// 但为了兼容性和处理操作失败的情况，保留这个检查
		if lockInfo.Completed {
			// 操作已完成：清理锁
			if lockInfo.Success {
				// 操作成功时锁应该已经被删除，这种情况不应该发生
				log.Printf("[TryLock] 操作已完成且成功: key=%s, 清理锁（不应该发生）", key)
			} else {
				// 操作已完成但失败：清理锁并分配锁给队列中的下一个节点，让它继续尝试
				log.Printf("[TryLock] 操作已完成但失败: key=%s, 处理队列", key)
				nextNodeID := lm.processQueue(shard, key)
				if nextNodeID != "" {
					lm.notifyLockAssigned(shard, key, nextNodeID)
				}
			}
			delete(shard.locks, key)
			// 3. 立即释放分段锁
			shard.mu.Unlock()
			// 返回 acquired=false, skip=false，让客户端继续等待或重试
			return false, false, ""
		} else {
			// 锁被占用但操作未完成
			// 如果当前请求的节点就是锁的持有者（可能是队列中的旧请求被分配了锁，现在客户端重新请求）
			if lockInfo.Request.NodeID == request.NodeID {
				// 注意：这里允许同一节点获取锁，但实际使用中，客户端应该在请求锁之前
				// 先检查引用计数（ShouldSkipOperation），如果资源已存在，不应该请求锁
				// 这个逻辑主要用于处理队列场景：队列中的旧请求被分配锁后，客户端通过轮询重新请求
				// 更新锁的请求信息（使用最新的请求）
				log.Printf("[TryLock] 同一节点重新请求: key=%s, node=%s, 更新锁信息",
					key, request.NodeID)
				lockInfo.Request = request
				lockInfo.AcquiredAt = time.Now()
				// 3. 立即释放分段锁
				shard.mu.Unlock()
				return true, false, ""
			}
			// 其他节点持有锁，加入等待队列
			log.Printf("[TryLock] 加入等待队列: key=%s, node=%s, 当前持有者=%s",
				key, request.NodeID, lockInfo.Request.NodeID)
			lm.addToQueue(shard, key, request)
			// 3. 立即释放分段锁
			shard.mu.Unlock()
			return false, false, ""
		}
	}

	// 资源锁不存在，创建新的资源锁
	log.Printf("[TryLock] 直接获取锁成功: key=%s, node=%s", key, request.NodeID)
	shard.locks[key] = &LockInfo{
		Request:    request,
		AcquiredAt: time.Now(),
		Completed:  false,
		Success:    false,
	}

	// 4. 立即释放分段锁
	shard.mu.Unlock()

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
	// Success 根据 Error 自动推断：没有 error 就是 success
	lockInfo.Success = (request.Error == "")
	lockInfo.CompletedAt = time.Now()

	if lockInfo.Success {
		// 操作成功：直接释放锁
		// 等待的节点通过SSE订阅已经收到事件，不需要保留锁
		// 如果资源已存在，客户端不会请求锁（请求前会检查）
		// 如果资源不存在，客户端会重新请求锁（此时锁已被清理，可以重新获取）
		log.Printf("[Unlock] 操作成功，释放锁: key=%s, node=%s", key, request.NodeID)

		// 触发订阅消息广播（在删除锁之前，确保订阅者能收到事件）
		lm.broadcastEvent(shard, key, &OperationEvent{
			Type:        request.Type,
			ResourceID:  request.ResourceID,
			NodeID:      request.NodeID,
			Success:     true,
			Error:       request.Error,
			CompletedAt: lockInfo.CompletedAt,
		})

		// 删除锁，避免内存泄漏
		delete(shard.locks, key)

		// 注意：不调用 processQueue，因为：
		// 1. 操作成功，资源已存在，队列中的节点不应该继续操作
		// 2. 队列中的节点通过SSE收到事件后，会重新检查资源
		// 3. 如果资源存在，不会请求锁；如果资源不存在，会重新请求锁（此时锁已被清理）
	} else {
		// 操作失败：删除锁并分配锁给队列中的下一个节点，让它继续尝试
		log.Printf("[Unlock] 操作失败，唤醒队列: key=%s, node=%s", key, request.NodeID)
		delete(shard.locks, key)
		nextNodeID := lm.processQueue(shard, key)

		// 如果成功分配锁给队头节点，通过SSE通知队头节点锁已被分配
		// 这样队头节点可以立即重新请求锁，而不需要等待定期检查
		if nextNodeID != "" {
			lm.notifyLockAssigned(shard, key, nextNodeID)
		}
	}

	return true
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
// 返回：分配锁的节点ID，如果没有队列则返回空字符串
func (lm *LockManager) processQueue(shard *resourceShard, key string) string {
	queue, exists := shard.queues[key]
	if !exists || len(queue) == 0 {
		return ""
	}

	// FIFO：取出队列中的第一个请求
	nextRequest := queue[0]
	shard.queues[key] = queue[1:]

	log.Printf("[processQueue] 从队列分配锁: key=%s, node=%s, 剩余队列长度=%d",
		key, nextRequest.NodeID, len(shard.queues[key]))

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

	return nextRequest.NodeID
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

// Subscribe 订阅资源操作完成事件
// 返回订阅者ID（用于取消订阅）
func (lm *LockManager) Subscribe(lockType, resourceID string, subscriber Subscriber) string {
	key := LockKey(lockType, resourceID)
	shard := lm.getShard(key)

	shard.mu.Lock()
	defer shard.mu.Unlock()

	if _, exists := shard.subscribers[key]; !exists {
		shard.subscribers[key] = make([]Subscriber, 0)
	}

	shard.subscribers[key] = append(shard.subscribers[key], subscriber)
	log.Printf("[Subscribe] 添加订阅者: key=%s, 当前订阅者数量=%d", key, len(shard.subscribers[key]))

	// 返回订阅者ID（使用内存地址作为唯一标识）
	return ""
}

// Unsubscribe 取消订阅
func (lm *LockManager) Unsubscribe(lockType, resourceID string, subscriber Subscriber) {
	key := LockKey(lockType, resourceID)
	shard := lm.getShard(key)

	shard.mu.Lock()
	defer shard.mu.Unlock()

	subscribers, exists := shard.subscribers[key]
	if !exists {
		return
	}

	// 从订阅者列表中移除
	for i, sub := range subscribers {
		if sub == subscriber {
			shard.subscribers[key] = append(subscribers[:i], subscribers[i+1:]...)
			log.Printf("[Unsubscribe] 移除订阅者: key=%s, 剩余订阅者数量=%d", key, len(shard.subscribers[key]))

			// 如果列表为空，删除该key
			if len(shard.subscribers[key]) == 0 {
				delete(shard.subscribers, key)
			}
			return
		}
	}
}

// broadcastEvent 广播事件给所有订阅者
// 注意：调用此函数时，shard.mu 必须已经加锁
func (lm *LockManager) broadcastEvent(shard *resourceShard, key string, event *OperationEvent) {
	subscribers, exists := shard.subscribers[key]
	if !exists || len(subscribers) == 0 {
		return
	}

	log.Printf("[BroadcastEvent] 广播事件: key=%s, 订阅者数量=%d, success=%v",
		key, len(subscribers), event.Success)

	// 清理无效的订阅者
	validSubscribers := make([]Subscriber, 0, len(subscribers))

	for _, sub := range subscribers {
		if err := sub.SendEvent(event); err != nil {
			log.Printf("[BroadcastEvent] 发送事件失败，移除订阅者: key=%s, error=%v", key, err)
			sub.Close()
		} else {
			validSubscribers = append(validSubscribers, sub)
		}
	}

	// 更新订阅者列表
	if len(validSubscribers) == 0 {
		delete(shard.subscribers, key)
	} else {
		shard.subscribers[key] = validSubscribers
	}
}

// notifyLockAssigned 通知队头节点锁已被分配
// 注意：调用此函数时，shard.mu 必须已经加锁
func (lm *LockManager) notifyLockAssigned(shard *resourceShard, key string, nodeID string) {
	subscribers, exists := shard.subscribers[key]
	if !exists || len(subscribers) == 0 {
		return
	}

	// 解析key获取type和resourceID
	parts := strings.Split(key, ":")
	if len(parts) < 2 {
		return
	}
	lockType := parts[0]
	resourceID := strings.Join(parts[1:], ":")

	// 创建"锁已分配"事件
	// 注意：Success=false 表示操作失败，但通过NodeID匹配，客户端可以知道锁已被分配给自己
	event := &OperationEvent{
		Type:        lockType,
		ResourceID:  resourceID,
		NodeID:      nodeID, // 队头节点的NodeID
		Success:     false,  // 操作失败
		Error:       "",     // 没有错误，只是通知锁已分配
		CompletedAt: time.Now(),
	}

	log.Printf("[notifyLockAssigned] 通知队头节点锁已分配: key=%s, node=%s, 订阅者数量=%d",
		key, nodeID, len(subscribers))

	// 发送事件给所有订阅者
	// 客户端收到事件后，检查NodeID是否匹配，如果匹配则重新请求锁
	validSubscribers := make([]Subscriber, 0, len(subscribers))

	for _, sub := range subscribers {
		if err := sub.SendEvent(event); err != nil {
			log.Printf("[notifyLockAssigned] 发送事件失败，移除订阅者: key=%s, error=%v", key, err)
			sub.Close()
		} else {
			validSubscribers = append(validSubscribers, sub)
		}
	}

	// 更新订阅者列表
	if len(validSubscribers) == 0 {
		delete(shard.subscribers, key)
	} else {
		shard.subscribers[key] = validSubscribers
	}
}

// 引用计数相关逻辑已移至 content 插件侧的 callback 使用中
