# LockInfo 处理流程分析（之前的代码）

## 问题

用户想知道：在之前的代码中，当 `processQueue` 创建了 `LockInfo` 之后：
```go
shard.locks[key] = &LockInfo{
    Request:    nextRequest,
    AcquiredAt: time.Now(),
    Completed:  false,
    Success:    false,
}
```

这个信息会被怎么处理？整个流程是什么？

## 之前的代码流程

### 1. 操作失败时的处理（Unlock）

**位置**：`server/lock_manager.go:186-201`（之前的代码）

```go
} else {
    // 操作失败：删除锁并分配锁给队列中的下一个节点，让它继续尝试
    log.Printf("[Unlock] 操作失败，唤醒队列: key=%s, node=%s", key, request.NodeID)
    delete(shard.locks, key)  // 1. 删除当前锁
    lm.processQueue(shard, key)  // 2. 分配锁给队头节点
    
    // 3. 触发订阅消息广播（操作失败）
    lm.broadcastEvent(shard, key, &OperationEvent{
        Type:        request.Type,
        ResourceID:  request.ResourceID,
        NodeID:      request.NodeID,  // 注意：这是失败节点的NodeID
        Success:     false,
        Error:       request.Error,
        CompletedAt: lockInfo.CompletedAt,
    })
}
```

**流程**：
1. ✅ 删除当前锁：`delete(shard.locks, key)`
2. ✅ 调用 `processQueue`：分配锁给队头节点
3. ✅ **广播失败事件**：通过SSE发送操作失败事件给所有订阅者

### 2. processQueue 创建 LockInfo

**位置**：`server/lock_manager.go:217-240`（之前的代码）

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
        Request:    nextRequest,  // 队头节点的请求
        AcquiredAt: time.Now(),
        Completed:  false,        // 新锁，操作还没开始
        Success:    false,        // 新锁，操作还没开始
    }
}
```

**关键点**：
- ✅ 创建新的 `LockInfo`，存储在 `shard.locks[key]` 中
- ✅ `Request` 设置为队头节点的请求（`nextRequest`）
- ✅ `Completed = false`，`Success = false`（因为操作还没开始）

### 3. LockInfo 的存储位置

**存储位置**：`shard.locks[key]`

```go
// resourceShard 结构
type resourceShard struct {
    mu sync.RWMutex
    locks map[string]*LockInfo  // ← LockInfo 存储在这里
    queues map[string][]*LockRequest
    subscribers map[string][]Subscriber
}
```

**关键点**：
- ✅ `LockInfo` 存储在 `shard.locks[key]` 中
- ✅ 这个 `LockInfo` 表示：**队头节点已经获得了锁，但操作还没开始**
- ✅ 其他节点请求锁时，会检查这个 `LockInfo`

### 4. 客户端如何发现锁已被分配

#### 方式1：定期检查（每秒）

**位置**：`client/client.go:148-169`（之前的代码）

```go
func (c *LockClient) waitForLock(ctx context.Context, request *Request) (*LockResult, error) {
    ticker := time.NewTicker(1 * time.Second)  // 每秒检查一次
    defer ticker.Stop()

    for {
        select {
        case <-ctx.Done():
            return nil, ctx.Err()
        case <-ticker.C:
            // 定期重新请求锁，检查锁是否已经被processQueue分配
            result, err := c.tryLockOnce(ctx, request)
            if err == nil && result.Acquired {
                return result, nil  // 获得锁，返回
            }
        }
        // ... SSE订阅逻辑
    }
}
```

**流程**：
1. 客户端每秒调用 `tryLockOnce`（发送 `/lock` 请求）
2. 服务端 `TryLock` 检查 `shard.locks[key]` 是否存在
3. 如果存在且 `NodeID` 匹配，返回 `acquired=true`

#### 方式2：SSE订阅等待事件（之前的代码）

**位置**：`client/client.go:292-400`（之前的代码）

```go
func (c *LockClient) handleOperationEvent(ctx context.Context, request *Request, event *OperationEvent) (*LockResult, bool, bool) {
    // 验证事件是否匹配当前请求
    if event.Type != request.Type || event.ResourceID != request.ResourceID {
        return nil, false, false
    }

    // 如果操作失败，再次尝试获取锁
    if !event.Success {
        // 等待100ms，确保服务端的 processQueue 已经完成锁的分配
        select {
        case <-ctx.Done():
            return nil, false, false
        case <-time.After(100 * time.Millisecond):
        }

        // 重新请求锁
        result, err := c.tryLockOnce(ctx, request)
        if err == nil && result.Acquired {
            return &LockResult{Acquired: true}, true, false
        }

        // 没有获得锁，说明其他节点已经获得了锁，需要重新订阅等待
        return nil, false, true
    }
    // ... 其他处理逻辑
}
```

**流程**：
1. 客户端收到操作失败事件（通过SSE）
2. 等待100ms，确保服务端的 `processQueue` 已经完成锁的分配
3. 重新请求锁（调用 `/lock`）
4. 服务端 `TryLock` 检查 `shard.locks[key]` 是否存在
5. 如果存在且 `NodeID` 匹配，返回 `acquired=true`

### 5. TryLock 如何处理 LockInfo

**位置**：`server/lock_manager.go:96-111`（之前的代码）

```go
// 锁被占用但操作未完成
if lockInfo.Request.NodeID == request.NodeID {
    // 同一节点重新请求
    log.Printf("[TryLock] 同一节点重新请求: key=%s, node=%s, 更新锁信息",
        key, request.NodeID)
    lockInfo.Request = request  // 更新请求信息（使用最新的请求）
    lockInfo.AcquiredAt = time.Now()
    shard.mu.Unlock()
    return true, false, ""  // ✅ 返回true，表示获得锁
}
```

**流程**：
1. 客户端重新请求锁（通过定期检查或SSE事件）
2. 服务端 `TryLock` 检查 `shard.locks[key]` 是否存在
3. 如果存在，检查 `lockInfo.Request.NodeID == request.NodeID`
4. 如果匹配，更新 `LockInfo`（使用最新的请求）
5. ✅ **返回 `true`，表示获得锁**

## 完整的流程时序图

### 之前的代码流程

```
T1: 节点A操作失败，调用Unlock
    → 服务端删除锁：delete(shard.locks, key)
    → 服务端调用processQueue：
       - 从队列中取出队头节点（节点B）的请求：nextRequest = queue[0]
       - 创建LockInfo：shard.locks[key] = &LockInfo{
           Request:    nextRequest,  // 节点B的请求
           AcquiredAt: time.Now(),
           Completed:  false,
           Success:    false,
         }
       - ✅ LockInfo 存储在 shard.locks[key] 中
    
T2: 服务端广播失败事件（之前的代码）
    → broadcastEvent 发送操作失败事件给所有订阅者
    → 事件包含：{NodeID: 节点A的NodeID, Success: false}
    
T3: 节点B收到失败事件（通过SSE）
    → handleOperationEvent 处理失败事件
    → 等待100ms，确保processQueue已经完成
    → 重新请求锁：调用 /lock
    
T4: 节点B调用 /lock
    → TryLock检查 shard.locks[key] 是否存在
    → ✅ LockInfo存在，检查 lockInfo.Request.NodeID == request.NodeID
    → ✅ NodeID匹配（都是节点B）
    → 更新LockInfo：lockInfo.Request = request（使用最新的请求）
    → ✅ 返回 true（获得锁）
    
T5: 节点B获得锁，继续操作
    → 返回 LockResult{Acquired: true}
    → 节点B开始执行操作
```

### 或者：节点B通过定期检查发现锁已被分配

```
T1: 节点A操作失败，调用Unlock
    → 服务端删除锁：delete(shard.locks, key)
    → 服务端调用processQueue：
       - 创建LockInfo：shard.locks[key] = &LockInfo{Request: 节点B的请求, ...}
       - ✅ LockInfo 存储在 shard.locks[key] 中
    
T2: 节点B在waitForLock中等待（SSE订阅中）
    → 定期检查（每秒）：调用 tryLockOnce
    
T3: 节点B调用 /lock（定期检查）
    → TryLock检查 shard.locks[key] 是否存在
    → ✅ LockInfo存在，检查 lockInfo.Request.NodeID == request.NodeID
    → ✅ NodeID匹配（都是节点B）
    → 更新LockInfo：lockInfo.Request = request（使用最新的请求）
    → ✅ 返回 true（获得锁）
    
T4: 节点B获得锁，继续操作
    → 返回 LockResult{Acquired: true}
    → 节点B开始执行操作
```

## LockInfo 的生命周期

### 创建阶段

```
processQueue 创建 LockInfo
    ↓
shard.locks[key] = &LockInfo{
    Request:    nextRequest,  // 队头节点的请求
    AcquiredAt: time.Now(),
    Completed:  false,
    Success:    false,
}
    ↓
LockInfo 存储在 shard.locks[key] 中
```

### 等待发现阶段

```
LockInfo 存储在 shard.locks[key] 中
    ↓
客户端通过两种方式发现：
    1. 定期检查（每秒调用 /lock）
    2. SSE订阅等待事件（收到失败事件后重新请求锁）
    ↓
TryLock 检查 LockInfo
    ↓
如果 NodeID 匹配，返回 true（获得锁）
```

### 更新阶段

```
客户端重新请求锁
    ↓
TryLock 检查 LockInfo
    ↓
如果 NodeID 匹配：
    lockInfo.Request = request  // 更新请求信息（使用最新的请求）
    lockInfo.AcquiredAt = time.Now()
    ↓
返回 true（获得锁）
```

### 使用阶段

```
客户端获得锁
    ↓
客户端执行操作
    ↓
操作完成后，调用 Unlock
    ↓
如果操作成功：
    delete(shard.locks, key)  // 删除LockInfo
    broadcastEvent 发送成功事件
    ↓
如果操作失败：
    delete(shard.locks, key)  // 删除LockInfo
    processQueue 创建新的LockInfo（给下一个节点）
    broadcastEvent 发送失败事件
```

## 关键点总结

### 1. LockInfo 的存储

- ✅ **存储位置**：`shard.locks[key]`
- ✅ **创建时机**：`processQueue` 分配锁给队头节点时
- ✅ **状态**：`Completed = false`，`Success = false`（因为操作还没开始）

### 2. LockInfo 的发现

- ✅ **方式1**：定期检查（每秒调用 `/lock`）
- ✅ **方式2**：SSE订阅等待事件（收到失败事件后重新请求锁）

### 3. LockInfo 的处理

- ✅ **TryLock 检查**：检查 `shard.locks[key]` 是否存在
- ✅ **NodeID 匹配**：如果 `lockInfo.Request.NodeID == request.NodeID`，返回 `true`
- ✅ **更新 LockInfo**：使用最新的请求更新 `LockInfo`

### 4. LockInfo 的删除

- ✅ **操作成功**：`delete(shard.locks, key)`，然后广播成功事件
- ✅ **操作失败**：`delete(shard.locks, key)`，然后 `processQueue` 创建新的 `LockInfo`

## 问题分析

### 为什么节点B可能显示"未获得锁"？

**可能的原因**：

1. **时间窗口问题**：
   - 节点B收到失败事件后立即调用 `/lock`
   - 但此时 `processQueue` 可能还没有完成锁的分配
   - **解决方案**：等待100ms（之前的代码已经实现）

2. **NodeID 不匹配**：
   - `processQueue` 使用队列中的旧请求对象分配锁
   - 节点B重新调用 `/lock` 时使用新的请求对象
   - 虽然 `NodeID` 应该匹配，但可能存在其他问题
   - **解决方案**：`TryLock` 检查 `NodeID` 匹配（之前的代码已经实现）

3. **锁已经被其他节点获取**：
   - 如果队列中有多个节点，节点B重新调用 `/lock` 时，锁可能已经被队列中的下一个节点获取了
   - **但是**：从日志看，剩余队列长度=1，说明节点B是队列中的第一个

## 总结

在之前的代码中，`processQueue` 创建 `LockInfo` 后的处理流程：

1. ✅ **创建**：`processQueue` 创建 `LockInfo`，存储在 `shard.locks[key]` 中
2. ✅ **广播**：`broadcastEvent` 发送操作失败事件给所有订阅者
3. ✅ **发现**：客户端通过定期检查或SSE事件发现锁已被分配
4. ✅ **请求**：客户端重新请求锁（调用 `/lock`）
5. ✅ **匹配**：`TryLock` 检查 `LockInfo`，如果 `NodeID` 匹配，返回 `true`
6. ✅ **更新**：更新 `LockInfo`（使用最新的请求）
7. ✅ **获得锁**：客户端获得锁，继续操作

**关键点**：
- `LockInfo` 存储在 `shard.locks[key]` 中
- 客户端通过定期检查或SSE事件发现锁已被分配
- `TryLock` 通过 `NodeID` 匹配判断是否获得锁

