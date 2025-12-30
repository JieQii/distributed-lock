# 物理锁实现最终方案

## 已确认的所有细节

### ✅ 1. 资源锁的初始化
- **延迟初始化**：第一次TryLock时创建锁
- **操作完成后立即释放**：操作成功时删除锁和资源锁

### ✅ 2. 锁的获取顺序
**流程**：
1. 获取分段锁
2. 检查资源锁是否存在
   - **如果存在**：获取引用 → **立即释放分段锁** → 获取资源锁
   - **如果不存在**：创建资源锁 → 加入resourceLocks map → **释放分段锁** → 获取资源锁
3. 获取资源锁
4. 重新获取分段锁访问`locks` map（因为`locks` map需要分段锁保护）

### ✅ 3. 操作失败后的处理方式
- **选择当前方案（SSE通知）**：操作失败 → 分配锁给下一个节点 → 通过SSE通知队头节点
- **不选择轮询方案**：效率低，可能造成泛洪

### ✅ 4. 不同操作类型的等待队列设计
- **确认当前设计**：不同操作类型不同队列
  - `queues["pull:resource123"]` - pull操作的等待队列
  - `queues["delete:resource123"]` - delete操作的等待队列
  - 不同操作类型互不干扰

### ✅ 5. 资源锁的生命周期（操作失败时）
- **操作失败时，保留资源锁**：下一个节点使用同一个资源锁，确保互斥
- **操作成功时，删除资源锁**：后续节点不需要锁（客户端会检查引用计数）

### ✅ 6. 物理锁的实际作用
- **支持更好的并发度**：不同资源可以并发获取锁
- **状态标记**：确保同一资源同一时刻只有一个操作在进行

---

## 最终实现方案

### 1. 数据结构

```go
type resourceShard struct {
    mu sync.RWMutex  // 分段锁（物理锁）
    
    // 资源锁（物理锁）：key -> Mutex
    resourceLocks map[string]*sync.Mutex
    
    // 锁状态：key -> LockInfo
    locks map[string]*LockInfo
    
    // 等待队列：key -> []*LockRequest (FIFO队列)
    // key = lockType:resourceID，不同操作类型不同队列
    queues map[string][]*LockRequest
    
    // 订阅者：key -> []Subscriber
    subscribers map[string][]Subscriber
}
```

### 2. TryLock实现

```go
func (lm *LockManager) TryLock(request *LockRequest) (bool, bool, string) {
    key := LockKey(request.Type, request.ResourceID)
    shard := lm.getShard(request.ResourceID)
    
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
                // 其他节点持有锁，加入等待队列
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
```

### 3. Unlock实现

```go
func (lm *LockManager) Unlock(request *UnlockRequest) bool {
    key := LockKey(request.Type, request.ResourceID)
    shard := lm.getShard(request.ResourceID)
    
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
```

### 4. 辅助函数

```go
// getOrCreateResourceLock 获取或创建资源锁
// 注意：调用此函数时，shard.mu 必须已经加锁
func (lm *LockManager) getOrCreateResourceLock(shard *resourceShard, key string) *sync.Mutex {
    if lock, exists := shard.resourceLocks[key]; exists {
        return lock
    }
    
    // 创建新的资源锁
    lock := &sync.Mutex{}
    shard.resourceLocks[key] = lock
    return lock
}
```

### 5. NewLockManager初始化

```go
func NewLockManager() *LockManager {
    lm := &LockManager{}
    // 初始化所有分段
    for i := 0; i < shardCount; i++ {
        lm.shards[i] = &resourceShard{
            resourceLocks: make(map[string]*sync.Mutex),  // 新增
            locks:          make(map[string]*LockInfo),
            queues:         make(map[string][]*LockRequest),
            subscribers:    make(map[string][]Subscriber),
        }
    }
    return lm
}
```

---

## 关键点总结

### 1. 锁的获取顺序
- ✅ 分段锁 → 检查/创建资源锁（访问resourceLocks map）→ 释放分段锁 → 获取资源锁 → 重新获取分段锁访问locks map

**为什么需要重新获取分段锁？**
- `resourceLocks` map 和 `locks` map 是分离的
- `resourceLocks` map 存储物理锁（sync.Mutex）
- `locks` map 存储锁状态（LockInfo）
- 访问 `locks` map 需要分段锁保护
- 所以获取资源锁后，需要重新获取分段锁来访问 `locks` map

### 2. 资源锁的生命周期
- ✅ 创建：第一次TryLock时
- ✅ 删除：操作成功时
- ✅ 保留：操作失败时（下一个节点使用）

### 3. 操作失败后的处理
- ✅ 保留资源锁
- ✅ 分配锁给队列中的下一个节点
- ✅ 通过SSE通知队头节点

### 4. 不同操作类型的队列
- ✅ 不同操作类型不同队列（`key = lockType:resourceID`）

---

## 下一步

所有细节已确认，可以开始实现代码了！

