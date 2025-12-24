# 操作失败后节点未获得锁的问题分析

## 问题描述

从日志看：
1. 节点A操作失败，调用Unlock
2. 服务端通过processQueue将锁分配给了节点B
3. 服务端广播操作失败事件
4. 节点B收到失败事件，重新调用/lock
5. **但是节点B显示"未获得锁"**

## 问题分析

### 时序流程

```
T1: 节点A操作失败，调用Unlock
    → 服务端删除锁：delete(shard.locks, key)
    → 服务端调用processQueue：分配锁给节点B
    → 服务端广播失败事件：broadcastEvent(...)
    
T2: 节点B收到失败事件（通过SSE）
    → handleOperationEvent处理失败事件
    → 重新调用 /lock 接口
    
T3: 节点B调用 /lock
    → TryLock检查锁是否存在
    → 如果锁已经被processQueue分配，应该返回true
    → 但是节点B显示"未获得锁"
```

### 可能的原因

#### 原因1：时间窗口问题

**问题**：节点B收到失败事件后立即调用/lock，但此时锁可能还没有被分配

**时序**：
```
T1: processQueue分配锁给节点B（在Unlock中）
T2: broadcastEvent广播事件（在Unlock中）
T3: 节点B收到事件，立即调用/lock
    → 可能存在时间窗口：锁还没有被分配
```

**但是**：从日志看，processQueue在broadcastEvent之前被调用，所以锁应该已经被分配了。

#### 原因2：节点B重新调用/lock时，锁已经被其他节点获取

**问题**：如果队列中有多个节点，节点B重新调用/lock时，锁可能已经被队列中的下一个节点获取了

**但是**：从日志看，剩余队列长度=1，说明节点B是队列中的第一个，锁应该已经被分配给它了。

#### 原因3：节点B重新调用/lock时，使用的请求对象与processQueue分配的不匹配

**问题**：processQueue使用队列中的旧请求对象分配锁，但节点B重新调用/lock时使用新的请求对象

**代码位置**：
- `processQueue`（第236行）：使用 `nextRequest`（队列中的旧请求）
- `handleOperationEvent`（第306行）：使用 `request`（新的请求对象）

**但是**：TryLock的第99行检查：`if lockInfo.Request.NodeID == request.NodeID`，这个应该能匹配上。

#### 原因4：节点B重新调用/lock时，锁已经被清理或不存在

**问题**：如果节点B重新调用/lock时，锁已经被清理或不存在，TryLock会创建新的锁

**但是**：从日志看，processQueue已经分配锁给节点B了，锁应该存在。

## 代码分析

### 服务端：Unlock方法

```go
// server/lock_manager.go:186-201
} else {
    // 操作失败：删除锁并分配锁给队列中的下一个节点，让它继续尝试
    log.Printf("[Unlock] 操作失败，唤醒队列: key=%s, node=%s", key, request.NodeID)
    delete(shard.locks, key)  // 1. 删除锁
    lm.processQueue(shard, key)  // 2. 分配锁给队列中的第一个节点
    
    // 3. 触发订阅消息广播（操作失败）
    lm.broadcastEvent(shard, key, &OperationEvent{
        Type:        request.Type,
        ResourceID:  request.ResourceID,
        NodeID:      request.NodeID,
        Success:     false,
        Error:       request.Error,
        CompletedAt: lockInfo.CompletedAt,
    })
}
```

**问题**：processQueue在broadcastEvent之前被调用，锁应该已经被分配了。

### 服务端：processQueue方法

```go
// server/lock_manager.go:217-240
func (lm *LockManager) processQueue(shard *resourceShard, key string) {
    // ...
    // FIFO：取出队列中的第一个请求
    nextRequest := queue[0]
    shard.queues[key] = queue[1:]
    
    // 分配锁给下一个请求
    shard.locks[key] = &LockInfo{
        Request:    nextRequest,  // 使用队列中的旧请求
        AcquiredAt: time.Now(),
        Completed:  false,
        Success:    false,
    }
}
```

**问题**：使用队列中的旧请求对象分配锁。

### 服务端：TryLock方法

```go
// server/lock_manager.go:99-110
if lockInfo.Request.NodeID == request.NodeID {
    // 同一节点重新请求
    log.Printf("[TryLock] 同一节点重新请求: key=%s, node=%s, 更新锁信息",
        key, request.NodeID)
    lockInfo.Request = request  // 更新请求信息
    lockInfo.AcquiredAt = time.Now()
    shard.mu.Unlock()
    return true, false, ""  // 返回true，表示获得锁
}
```

**问题**：如果节点B重新调用/lock时，锁已经被分配给它，应该能匹配上这个条件，返回true。

### 客户端：handleOperationEvent方法

```go
// client/client.go:289-357
// 如果操作失败，再次尝试获取锁
// ...
req, err := http.NewRequestWithContext(ctx, "POST", c.ServerURL+"/lock", bytes.NewBuffer(jsonData))
// ...
resp, err := c.Client.Do(req)
// ...
var lockResp LockResponse
// ...
if lockResp.Acquired {
    return &LockResult{
        Acquired: true,
    }, true, false
}

// 没有获得锁，说明其他节点已经获得了锁，需要重新订阅等待
return nil, false, true
```

**问题**：如果节点B重新调用/lock时，锁已经被分配给它，应该返回 `Acquired: true`。

## 可能的问题

### 问题1：节点B重新调用/lock时，锁还没有被分配（时间窗口）

**场景**：
- processQueue在Unlock中被调用，分配锁给节点B
- 但是，节点B收到失败事件后立即调用/lock
- 可能存在时间窗口：锁还没有被分配

**但是**：从日志看，processQueue在broadcastEvent之前被调用，所以锁应该已经被分配了。

### 问题2：节点B重新调用/lock时，锁已经被其他节点获取

**场景**：
- 如果队列中有多个节点，节点B重新调用/lock时，锁可能已经被队列中的下一个节点获取了

**但是**：从日志看，剩余队列长度=1，说明节点B是队列中的第一个，锁应该已经被分配给它了。

### 问题3：节点B重新调用/lock时，使用的请求对象与processQueue分配的不匹配

**场景**：
- processQueue使用队列中的旧请求对象分配锁
- 节点B重新调用/lock时使用新的请求对象
- TryLock检查时，虽然NodeID匹配，但可能存在其他问题

**但是**：TryLock的第99行检查：`if lockInfo.Request.NodeID == request.NodeID`，这个应该能匹配上。

## 解决方案

### 方案1：在processQueue中不立即分配锁，而是等待节点重新请求

**问题**：这会导致节点需要重新请求锁，增加延迟。

### 方案2：确保节点B重新调用/lock时，锁已经被分配

**问题**：可能存在时间窗口问题。

### 方案3：修改handleOperationEvent，如果节点是队列中的第一个，直接返回获得锁

**问题**：客户端无法知道自己是队列中的第一个。

### 方案4：修改processQueue，不立即分配锁，而是通过事件通知节点重新请求

**问题**：这会导致节点需要重新请求锁，增加延迟。

## 建议的解决方案

### 方案：在handleOperationEvent中，如果操作失败，直接返回需要重新订阅，让节点重新请求锁

**修改**：
```go
// 如果操作失败，不立即重新请求锁，而是返回需要重新订阅
// 让节点重新请求锁，此时锁应该已经被processQueue分配了
return nil, false, true
```

**但是**：这会导致节点需要重新订阅，增加延迟。

### 方案：修改processQueue，不立即分配锁，而是通过事件通知节点重新请求

**修改**：
```go
// processQueue不立即分配锁，而是等待节点重新请求
// 节点收到失败事件后，重新请求锁，此时锁应该已经被分配了
```

**但是**：这会导致节点需要重新请求锁，增加延迟。

## 最佳解决方案

### 方案：确保节点B重新调用/lock时，锁已经被分配

**修改**：
1. 在processQueue中，确保锁已经被分配
2. 在broadcastEvent中，确保事件已经被广播
3. 在handleOperationEvent中，如果操作失败，等待一小段时间后再重新请求锁

**但是**：这会导致延迟增加。

### 方案：修改handleOperationEvent，如果操作失败，直接返回需要重新订阅

**修改**：
```go
// 如果操作失败，不立即重新请求锁，而是返回需要重新订阅
// 让节点重新请求锁，此时锁应该已经被processQueue分配了
return nil, false, true
```

**优势**：
- 简单直接
- 不需要修改服务端逻辑
- 节点重新请求锁时，锁应该已经被分配了

**问题**：
- 节点需要重新订阅，增加延迟
- 但是，如果锁已经被分配，节点重新请求锁时会立即获得锁

## 结论

问题可能在于：节点B收到失败事件后立即调用/lock，但此时锁可能还没有被分配（时间窗口问题）。

建议的解决方案：修改handleOperationEvent，如果操作失败，直接返回需要重新订阅，让节点重新请求锁。这样，节点重新请求锁时，锁应该已经被processQueue分配了。

