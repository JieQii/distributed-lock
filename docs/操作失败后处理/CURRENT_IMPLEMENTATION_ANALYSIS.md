# 当前实现分析：操作失败后锁分配逻辑

## 用户的问题

用户想知道当前的逻辑是否是：
1. 当操作失败后，可以从资源的waiter队列中获得队头的请求
2. 然后把资源锁的holder设置为队头的请求
3. 然后**直接返回加锁成功给该请求**

## 当前实现的逻辑

### 1. 操作失败时的处理（Unlock）

**位置**：`server/lock_manager.go:186-196`

```go
} else {
    // 操作失败：删除锁并分配锁给队列中的下一个节点，让它继续尝试
    log.Printf("[Unlock] 操作失败，唤醒队列: key=%s, node=%s", key, request.NodeID)
    delete(shard.locks, key)  // 1. 删除当前锁
    lm.processQueue(shard, key)  // 2. 分配锁给队头节点
    
    // 注意：不广播失败事件
}
```

**流程**：
1. ✅ 删除当前锁：`delete(shard.locks, key)`
2. ✅ 调用 `processQueue`：分配锁给队头节点

### 2. processQueue 的实现

**位置**：`server/lock_manager.go:210-237`

```go
func (lm *LockManager) processQueue(shard *resourceShard, key string) {
    queue, exists := shard.queues[key]
    if !exists || len(queue) == 0 {
        return
    }
    
    // FIFO：取出队列中的第一个请求
    nextRequest := queue[0]  // 1. 从队列获取队头请求 ✅
    shard.queues[key] = queue[1:]
    
    // 分配锁给下一个请求
    shard.locks[key] = &LockInfo{
        Request:    nextRequest,  // 2. 设置锁的holder为队头请求 ✅
        AcquiredAt: time.Now(),
        Completed:  false,
        Success:    false,
    }
    // 3. ❌ 不是直接返回加锁成功，而是创建LockInfo
}
```

**流程**：
1. ✅ **从队列获取队头请求**：`nextRequest = queue[0]`
2. ✅ **设置锁的holder为队头请求**：`shard.locks[key] = &LockInfo{Request: nextRequest, ...}`
3. ❌ **不是直接返回加锁成功**：只是创建LockInfo，设置锁的holder

### 3. TryLock 的实现（队头节点重新请求锁时）

**位置**：`server/lock_manager.go:96-111`

```go
// 锁被占用但操作未完成
if lockInfo.Request.NodeID == request.NodeID {
    // 同一节点重新请求
    log.Printf("[TryLock] 同一节点重新请求: key=%s, node=%s, 更新锁信息",
        key, request.NodeID)
    lockInfo.Request = request
    lockInfo.AcquiredAt = time.Now()
    shard.mu.Unlock()
    return true, false, ""  // ✅ 返回true，表示获得锁
}
```

**流程**：
1. 队头节点重新请求锁（通过定期检查或SSE连接断开）
2. TryLock检查锁是否存在：`if lockInfo, exists := shard.locks[key]`
3. 检查是否是同一节点：`if lockInfo.Request.NodeID == request.NodeID`
4. ✅ **返回true（获得锁）**

## 对比：用户期望 vs 当前实现

| 步骤 | 用户期望的逻辑 | 当前实现 | 状态 |
|------|--------------|---------|------|
| **1. 操作失败** | ✅ 操作失败 | ✅ 操作失败 | ✅ 已实现 |
| **2. 从队列获取队头请求** | ✅ 从队列获取队头请求 | ✅ `nextRequest = queue[0]` | ✅ 已实现 |
| **3. 设置锁的holder** | ✅ 设置锁的holder为队头请求 | ✅ `shard.locks[key] = &LockInfo{Request: nextRequest, ...}` | ✅ 已实现 |
| **4. 直接返回加锁成功** | ✅ **直接返回加锁成功给该请求** | ❌ **需要节点重新请求锁** | ❌ **未实现** |

## 当前实现的完整流程

### 操作失败后的流程

```
T1: 节点A操作失败，调用Unlock
    → 服务端删除锁：delete(shard.locks, key)
    → 服务端调用processQueue：
       - 从队列中取出队头节点（节点B）的请求：nextRequest = queue[0] ✅
       - 创建LockInfo：shard.locks[key] = &LockInfo{Request: 节点B的请求, ...} ✅
       - 锁的holder被设置为节点B的请求 ✅
       - ❌ 不是直接返回加锁成功
    
T2: 节点B重新请求锁（通过定期检查或SSE连接断开）
    → TryLock检查锁是否存在：lockInfo, exists := shard.locks[key]
    → 锁存在，检查是否是同一节点：lockInfo.Request.NodeID == request.NodeID
    → NodeID匹配，返回true（获得锁）✅
    
T3: 节点B获得锁，继续操作
```

## 问题分析

### 为什么不能直接返回加锁成功？

**原因**：
1. **processQueue是在Unlock中被调用的**：
   - Unlock是一个HTTP请求处理函数
   - 此时队头节点（节点B）可能还在SSE订阅中等待
   - 没有活跃的HTTP连接可以返回加锁成功

2. **队头节点没有主动请求锁**：
   - 队头节点在SSE订阅中等待操作成功事件
   - 没有发送HTTP请求，无法直接返回加锁成功

3. **异步通信的限制**：
   - SSE是单向的（服务端→客户端）
   - 无法通过SSE直接返回加锁成功

### 当前实现的解决方案

**方案**：队头节点通过定期检查或SSE连接断开时重新请求锁

1. **定期检查**（每秒）：
   - 队头节点定期重新请求锁
   - TryLock检查到锁存在且NodeID匹配，返回true

2. **SSE连接断开时**：
   - SSE连接断开时，重新请求锁
   - TryLock检查到锁存在且NodeID匹配，返回true

## 结论

### 当前实现的逻辑

1. ✅ **操作失败后，从队列获取队头请求**：`processQueue`从队列中取出队头节点的请求
2. ✅ **设置锁的holder为队头请求**：创建LockInfo，设置Request为队头节点的请求
3. ❌ **不是直接返回加锁成功**：需要节点重新请求锁，TryLock检查到锁存在且NodeID匹配，返回true

### 用户期望的逻辑

用户期望：操作失败后，**直接返回加锁成功给队头节点的请求**。

**但是**：当前实现中，processQueue是在Unlock中被调用的，此时队头节点可能还在SSE订阅中等待，无法直接返回加锁成功。

### 当前实现的合理性

**当前实现是合理的**：
- processQueue分配锁给队头节点（设置锁的holder）
- 队头节点通过定期检查（每秒）或SSE连接断开时重新请求锁
- TryLock检查到锁存在且NodeID匹配，返回true（获得锁）

**延迟**：
- 最多1秒延迟（定期检查间隔）
- 如果SSE连接断开，立即重新请求锁

### 如果需要"直接返回加锁成功"

**需要修改架构**：
1. 操作失败时，不通过processQueue分配锁
2. 而是通过SSE直接通知队头节点
3. 队头节点收到通知后，重新请求锁

**但是**：这需要知道哪个节点是队头节点，并且需要单独通知队头节点，实现复杂度较高。

## 建议

**当前实现是合理的**：
- ✅ 操作失败后，从队列获取队头请求
- ✅ 设置锁的holder为队头请求
- ✅ 队头节点通过重新请求锁来获得锁（最多1秒延迟）

**如果需要优化**：
- 可以减少定期检查的间隔（例如500ms）
- 或者在SSE连接断开时立即重新请求锁（已实现）

