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
	mu sync.RWMutex // 分段锁，保护资源锁的创建和访问

	// 资源锁（物理锁）：key -> Mutex
	// key = lockType:resourceID，每个资源一个真实的互斥锁
	resourceLocks map[string]*sync.Mutex

	// 锁状态：key -> LockInfo
	// key = lockType:resourceID，存储锁的状态信息
	locks map[string]*LockInfo

	// 等待队列：key -> []*LockRequest (FIFO队列)
	// key = lockType:resourceID，不同操作类型不同队列
	queues map[string][]*LockRequest

	// 订阅者：key -> []Subscriber
	// key = lockType:resourceID
	subscribers map[string][]Subscriber
}

// LockManager 锁管理器
// 使用分段锁提升并发度：不同资源可以并发访问，只有相同分段的资源才会竞争
type LockManager struct {
	shards [shardCount]*resourceShard

	// AllowMultiNodeDownload 是否允许多节点下载模式
	// true:  允许多节点下载，锁被占用时加入等待队列
	// false: 禁止多节点下载，锁被占用时直接返回失败
	AllowMultiNodeDownload bool
}

// getShard 根据resourceID获取对应的分段
// 注意：分段只根据resourceID，不包含操作类型，确保同一镜像层的所有操作类型互斥
func (lm *LockManager) getShard(resourceID string) *resourceShard {
	// 使用FNV-1a哈希算法计算分段索引
	// 只对resourceID进行哈希，确保同一镜像层的所有操作类型（pull、update、delete）分到同一个分段
	h := fnv.New32a()
	h.Write([]byte(resourceID))
	return lm.shards[h.Sum32()%shardCount]
}

// NewLockManager 创建新的锁管理器
// allowMultiNodeDownload: 是否允许多节点下载模式
//   - true:  允许多节点下载，锁被占用时加入等待队列
//   - false: 禁止多节点下载，锁被占用时直接返回失败
func NewLockManager(allowMultiNodeDownload bool) *LockManager {
	lm := &LockManager{
		AllowMultiNodeDownload: allowMultiNodeDownload,
	}
	// 初始化所有分段
	for i := 0; i < shardCount; i++ {
		lm.shards[i] = &resourceShard{
			resourceLocks: make(map[string]*sync.Mutex), // 资源锁（物理锁）
			locks:         make(map[string]*LockInfo),
			queues:        make(map[string][]*LockRequest),
			subscribers:   make(map[string][]Subscriber),
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
	shard := lm.getShard(request.ResourceID) // 获取对应的分段（只根据resourceID分段，确保同一镜像层的所有操作类型互斥）

	request.Timestamp = time.Now()

	// ========== 阶段1：获取分段锁，检查/创建资源锁 ==========
	shard.mu.Lock()

	var resourceLock *sync.Mutex
	if existingLock, exists := shard.resourceLocks[key]; exists {
		// 资源锁存在：获取引用，立即释放分段锁
		resourceLock = existingLock
		shard.mu.Unlock()
	} else {
		// 资源锁不存在：创建资源锁，加入map，释放分段锁
		resourceLock = &sync.Mutex{}
		shard.resourceLocks[key] = resourceLock
		shard.mu.Unlock()
	}

	// ========== 阶段2：获取资源锁 ==========
	resourceLock.Lock()
	defer resourceLock.Unlock()

	// ========== 阶段3：重新获取分段锁访问locks map ==========
	shard.mu.RLock()
	lockInfo, exists := shard.locks[key]
	shard.mu.RUnlock()

	// ========== 阶段4：根据检查结果处理 ==========
	if exists {
		// 锁已存在
		if lockInfo.Completed {
			// 操作已完成（不应该发生，但保留检查）
			if lockInfo.Success {
				// 操作成功时锁应该已经被删除，这种情况不应该发生
				log.Printf("[TryLock] 操作已完成且成功: key=%s, 清理锁（不应该发生）", key)
			} else {
				// 操作已完成但失败：清理锁并分配锁给队列中的下一个节点
				log.Printf("[TryLock] 操作已完成但失败: key=%s, 处理队列", key)
				shard.mu.Lock()
				nextNodeID := lm.processQueue(shard, key)
				shard.mu.Unlock()
				if nextNodeID != "" {
					shard.mu.Lock()
					lm.notifyLockAssigned(shard, key, nextNodeID)
					shard.mu.Unlock()
				}
			}
			shard.mu.Lock()
			delete(shard.locks, key)
			shard.mu.Unlock()
			return false, false, ""
		} else {
			// 锁被占用但操作未完成
			if lockInfo.Request.NodeID == request.NodeID {
				// 同一节点重新请求（队列场景）
				log.Printf("[TryLock] 同一节点重新请求: key=%s, node=%s, 更新锁信息",
					key, request.NodeID)
				shard.mu.Lock()
				lockInfo.Request = request
				lockInfo.AcquiredAt = time.Now()
				shard.mu.Unlock()
				return true, false, ""
			} else {
				// 其他节点持有锁
				if !lm.AllowMultiNodeDownload {
					// 多节点下载模式关闭：直接返回失败，不加入队列
					log.Printf("[TryLock] 多节点下载已关闭，锁被占用: key=%s, node=%s, 当前持有者=%s",
						key, request.NodeID, lockInfo.Request.NodeID)
					return false, false, "多节点下载模式已关闭，锁已被其他节点占用"
				}
				// 多节点下载模式开启：加入等待队列
				log.Printf("[TryLock] 加入等待队列: key=%s, node=%s, 当前持有者=%s",
					key, request.NodeID, lockInfo.Request.NodeID)
				shard.mu.Lock()
				lm.addToQueue(shard, key, request)
				shard.mu.Unlock()
				return false, false, ""
			}
		}
	} else {
		// 锁不存在，创建新的资源锁
		log.Printf("[TryLock] 直接获取锁成功: key=%s, node=%s", key, request.NodeID)
		shard.mu.Lock()
		shard.locks[key] = &LockInfo{
			Request:    request,
			AcquiredAt: time.Now(),
			Completed:  false,
			Success:    false,
		}
		shard.mu.Unlock()
		return true, false, ""
	}
}

// Unlock 释放锁
func (lm *LockManager) Unlock(request *UnlockRequest) bool {
	key := LockKey(request.Type, request.ResourceID)
	shard := lm.getShard(request.ResourceID) // 获取对应的分段（只根据resourceID分段，确保同一镜像层的所有操作类型互斥）

	// ========== 阶段1：获取分段锁，获取资源锁引用 ==========
	shard.mu.Lock()
	resourceLock, exists := shard.resourceLocks[key]
	shard.mu.Unlock()

	if !exists {
		return false
	}

	// ========== 阶段2：获取资源锁 ==========
	resourceLock.Lock()
	defer resourceLock.Unlock()

	// ========== 阶段3：重新获取分段锁访问locks map ==========
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
		// ========== 操作成功：删除锁和资源锁 ==========
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

		// 删除锁和资源锁
		delete(shard.locks, key)
		delete(shard.resourceLocks, key)

		// 注意：不调用 processQueue，因为：
		// 1. 操作成功，资源已存在，队列中的节点不应该继续操作
		// 2. 队列中的节点通过SSE收到事件后，会重新检查资源
		// 3. 如果资源存在，不会请求锁；如果资源不存在，会重新请求锁（此时锁已被清理）
	} else {
		// ========== 操作失败：保留资源锁，分配锁给队列中的下一个节点 ==========
		log.Printf("[Unlock] 操作失败，唤醒队列: key=%s, node=%s", key, request.NodeID)

		// 删除锁状态（但保留资源锁）
		delete(shard.locks, key)

		// 分配锁给队列中的下一个节点
		nextNodeID := lm.processQueue(shard, key)

		// 通过SSE通知队头节点锁已被分配
		if nextNodeID != "" {
			lm.notifyLockAssigned(shard, key, nextNodeID)
		}

		// 注意：资源锁保留，下一个节点使用同一个资源锁
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
	shard := lm.getShard(resourceID) // 获取对应的分段（只根据resourceID分段）

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
	shard := lm.getShard(resourceID) // 获取对应的分段（只根据resourceID分段）

	shard.mu.RLock()
	defer shard.mu.RUnlock()

	return shard.locks[key]
}

// Subscribe 订阅资源操作完成事件
// 返回订阅者ID（用于取消订阅）
func (lm *LockManager) Subscribe(lockType, resourceID string, subscriber Subscriber) string {
	key := LockKey(lockType, resourceID)
	shard := lm.getShard(resourceID) // 获取对应的分段（只根据resourceID分段）

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
	shard := lm.getShard(resourceID) // 获取对应的分段（只根据resourceID分段）

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
