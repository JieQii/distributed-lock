# 操作失败后锁分配逻辑分析

## 用户的问题

用户想知道当前的逻辑是否是：
- 当操作失败后，可以从资源的waiter队列中获得队头的请求
- 然后把资源锁的holder设置为队头的请求
- 然后**直接返回加锁成功给该请求**

## 当前实现分析

### 1. 操作失败时的处理（Unlock）

**位置**：`server/lock_manager.go:186-196`

```go
} else {
    // 操作失败：删除锁并分配锁给队列中的下一个节点，让它继续尝试
    log.Printf("[Unlock] 操作失败，唤醒队列: key=%s, node=%s", key, request.NodeID)
    delete(shard.locks, key)
    lm.processQueue(shard, key)  // 分配锁给队头节点
    
    // 注意：不广播失败事件
}
```

**流程**：
1. 删除当前锁：`delete(shard.locks, key)`
2. 调用 `processQueue`：分配锁给队头节点
3. **不广播失败事件**

### 2. processQueue 的实现

**位置**：`server/lock_manager.go:215-237`

```go
func (lm *LockManager) processQueue(shard *resourceShard, key string) {
    queue, exists := shard.queues[key]
    if !exists || len(queue) == 0 {
        return
    }
    
    // FIFO：取出队列中的第一个请求
    nextRequest := queue[0]
    shard.queues[key] = queue[1:]
    
    // 分配锁给下一个请求
    shard.locks[key] = &LockInfo{
        Request:    nextRequest,  // 设置锁的holder为队头节点的请求
        AcquiredAt: time.Now(),
        Completed:  false,
        Success:    false,
    }
}
```

**流程**：
1. 从队列中取出第一个请求：`nextRequest = queue[0]`
2. 创建新的LockInfo：`shard.locks[key] = &LockInfo{Request: nextRequest, ...}`
3. **锁的holder被设置为队头节点的请求**

### 3. TryLock 的实现

**位置**：`server/lock_manager.go:67-135`

```go
func (lm *LockManager) TryLock(request *LockRequest) (bool, bool, string) {
    // ...
    
    // 检查资源锁是否存在
    if lockInfo, exists := shard.locks[key]; exists {
        // 锁被占用但操作未完成
        if lockInfo.Request.NodeID == request.NodeID {
            // 同一节点重新请求
            log.Printf("[TryLock] 同一节点重新请求: key=%s, node=%s, 更新锁信息",
                key, request.NodeID)
            lockInfo.Request = request
            lockInfo.AcquiredAt = time.Now()
            shard.mu.Unlock()
            return true, false, ""  // 返回true，表示获得锁
        }
        // 其他节点持有锁，加入等待队列
        // ...
    }
    
    // 资源锁不存在，创建新的资源锁
    // ...
}
```

**流程**：
1. 检查锁是否存在：`if lockInfo, exists := shard.locks[key]`
2. 如果锁存在且操作未完成：
   - 检查是否是同一节点：`if lockInfo.Request.NodeID == request.NodeID`
   - 如果是同一节点，返回 `true`（获得锁）

## 当前实现的逻辑

### 操作失败后的完整流程

```
T1: 节点A操作失败，调用Unlock
    → 服务端删除锁：delete(shard.locks, key)
    → 服务端调用processQueue：
       - 从队列中取出队头节点（节点B）的请求
       - 创建LockInfo：shard.locks[key] = &LockInfo{Request: 节点B的请求, ...}
       - 锁的holder被设置为节点B的请求 ✅
    
T2: 节点B重新请求锁（通过定期检查或SSE连接断开）
    → TryLock检查锁是否存在：lockInfo, exists := shard.locks[key]
    → 锁存在，检查是否是同一节点：lockInfo.Request.NodeID == request.NodeID
    → NodeID匹配，返回true（获得锁）✅
    
T3: 节点B获得锁，继续操作
```

### 关键点

1. **processQueue 分配锁**：
   - ✅ 从队列中取出队头节点的请求
   - ✅ 创建LockInfo，设置Request为队头节点的请求
   - ✅ 锁的holder被设置为队头节点的请求

2. **TryLock 检查锁**：
   - ✅ 检查锁是否存在
   - ✅ 检查是否是同一节点
   - ✅ 如果是同一节点，返回true（获得锁）

3. **但是**：
   - ❌ **不是直接返回加锁成功给该请求**
   - ⚠️ **需要节点B重新请求锁**（通过定期检查或SSE连接断开）

## 用户期望的逻辑 vs 当前实现

| 步骤 | 用户期望的逻辑 | 当前实现 |
|------|--------------|---------|
| **1. 操作失败** | ✅ 操作失败 | ✅ 操作失败 |
| **2. 从队列获取队头请求** | ✅ 从队列获取队头请求 | ✅ processQueue获取队头请求 |
| **3. 设置锁的holder** | ✅ 设置锁的holder为队头请求 | ✅ 创建LockInfo，设置Request为队头请求 |
| **4. 直接返回加锁成功** | ✅ **直接返回加锁成功给该请求** | ❌ **需要节点重新请求锁** |

## 问题分析

### 当前实现的问题

**问题**：processQueue分配锁后，**不会直接返回加锁成功给队头节点**，而是需要节点重新请求锁。

**原因**：
- processQueue只是创建LockInfo，设置锁的holder
- 没有主动通知队头节点
- 队头节点需要重新请求锁（通过定期检查或SSE连接断开）

### 用户期望的逻辑

**期望**：操作失败后，processQueue分配锁给队头节点，**直接返回加锁成功给该请求**。

**问题**：processQueue是在Unlock中被调用的，此时队头节点可能还在SSE订阅中等待，无法直接返回加锁成功。

## 可能的解决方案

### 方案1：保持当前实现（需要节点重新请求锁）

**当前实现**：
- processQueue分配锁给队头节点
- 队头节点通过定期检查或SSE连接断开时重新请求锁
- TryLock检查到锁存在且NodeID匹配，返回true

**优势**：
- 实现简单
- 不需要主动通知机制

**问题**：
- 需要节点重新请求锁，有延迟（最多1秒）

### 方案2：操作失败时广播事件给队头节点

**实现**：
- processQueue分配锁给队头节点
- 只通知队头节点（不广播给所有订阅者）
- 队头节点收到事件后，重新请求锁

**优势**：
- 队头节点能及时知道可以重新请求锁
- 其他节点不需要知道操作失败

**问题**：
- 需要知道哪个节点是队头节点
- 需要单独通知队头节点

### 方案3：操作失败时直接返回加锁成功（如果可能）

**实现**：
- processQueue分配锁给队头节点
- 如果队头节点有活跃的HTTP连接，直接返回加锁成功

**问题**：
- 队头节点可能没有活跃的HTTP连接（在SSE订阅中等待）
- 无法直接返回加锁成功

## 结论

### 当前实现的逻辑

1. ✅ **操作失败后，从队列获取队头请求**：processQueue从队列中取出队头节点的请求
2. ✅ **设置锁的holder为队头请求**：创建LockInfo，设置Request为队头节点的请求
3. ❌ **不是直接返回加锁成功**：需要节点重新请求锁，TryLock检查到锁存在且NodeID匹配，返回true

### 用户期望的逻辑

用户期望：操作失败后，**直接返回加锁成功给队头节点的请求**。

**但是**：当前实现中，processQueue是在Unlock中被调用的，此时队头节点可能还在SSE订阅中等待，无法直接返回加锁成功。

### 建议

**当前实现是合理的**：
- processQueue分配锁给队头节点
- 队头节点通过定期检查（每秒）或SSE连接断开时重新请求锁
- TryLock检查到锁存在且NodeID匹配，返回true（获得锁）

**如果需要优化**：
- 可以减少定期检查的间隔（例如500ms）
- 或者在SSE连接断开时立即重新请求锁（已实现）

