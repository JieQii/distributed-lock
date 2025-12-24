# SSE通知解决方案：操作失败后立即通知队头节点

## 问题

用户问：**队头节点可能没有活跃的HTTP连接（在SSE订阅中等待），这个问题能解决吗？**

## 解决方案

**可以解决！** 通过SSE（Server-Sent Events）通知队头节点锁已被分配。

### 实现原理

1. **操作失败时**：
   - 服务端调用 `processQueue` 分配锁给队头节点
   - 服务端调用 `notifyLockAssigned` 通过SSE通知队头节点

2. **队头节点收到SSE事件**：
   - 检查事件的 `NodeID` 是否匹配当前节点
   - 如果匹配，立即重新请求锁（不需要等待定期检查）
   - 如果不匹配，继续等待

## 代码实现

### 1. 服务端：修改 `processQueue` 返回节点ID

**位置**：`server/lock_manager.go:212-238`

```go
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

    // 分配锁给下一个请求
    shard.locks[key] = &LockInfo{
        Request:    nextRequest,
        AcquiredAt: time.Now(),
        Completed:  false,
        Success:    false,
    }

    return nextRequest.NodeID  // 返回节点ID
}
```

### 2. 服务端：添加 `notifyLockAssigned` 函数

**位置**：`server/lock_manager.go:346-400`

```go
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
```

### 3. 服务端：在 `Unlock` 中调用 `notifyLockAssigned`

**位置**：`server/lock_manager.go:186-197`

```go
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
```

### 4. 客户端：修改 `handleOperationEvent` 识别"锁已分配"事件

**位置**：`client/client.go:322-340`

```go
// 如果操作失败，检查是否是"锁已分配"事件
// 说明：当获得锁的节点操作失败时，服务端会：
// 1. 删除锁
// 2. 通过 processQueue 将锁分配给等待队列中的第一个节点（FIFO）
// 3. 通过 notifyLockAssigned 发送事件通知队头节点锁已被分配
//
// 如果事件的NodeID匹配当前节点，说明锁已被分配给自己，应该立即重新请求锁
// 如果事件的NodeID不匹配当前节点，说明锁被分配给了其他节点，需要继续等待
if event.NodeID == request.NodeID {
    // 锁已被分配给自己，立即重新请求锁
    // 不需要等待，因为服务端已经完成了锁的分配
} else {
    // 锁被分配给了其他节点，继续等待
    return nil, false, true // 重新订阅，继续等待
}

// 立即重新请求锁（不需要等待100ms）
jsonData, err := json.Marshal(request)
// ... 重新请求锁的逻辑
```

## 工作流程

### 操作失败后的完整流程

```
T1: 节点A操作失败，调用Unlock
    → 服务端删除锁：delete(shard.locks, key)
    → 服务端调用processQueue：
       - 从队列中取出队头节点（节点B）的请求
       - 创建LockInfo：shard.locks[key] = &LockInfo{Request: 节点B的请求, ...}
       - 返回节点B的NodeID
    
T2: 服务端调用notifyLockAssigned：
    - 创建"锁已分配"事件：event = {NodeID: 节点B的NodeID, Success: false, Error: ""}
    - 通过SSE发送事件给所有订阅者
    
T3: 节点B收到SSE事件：
    - 检查event.NodeID == request.NodeID
    - NodeID匹配，立即重新请求锁（不需要等待定期检查）
    
T4: 节点B重新请求锁：
    → TryLock检查锁是否存在：lockInfo, exists := shard.locks[key]
    → 锁存在，检查是否是同一节点：lockInfo.Request.NodeID == request.NodeID
    → NodeID匹配，返回true（获得锁）✅
    
T5: 节点B获得锁，继续操作
```

## 优势

1. **立即通知**：队头节点通过SSE立即收到"锁已分配"事件，不需要等待定期检查（最多1秒延迟）
2. **精确匹配**：通过NodeID匹配，只有队头节点会重新请求锁，其他节点继续等待
3. **无需修改订阅接口**：不需要修改订阅接口添加节点ID参数，通过事件中的NodeID匹配即可

## 对比：之前 vs 现在

| 方案 | 延迟 | 实现复杂度 |
|------|------|-----------|
| **之前**：定期检查（每秒） | 最多1秒 | 简单 |
| **现在**：SSE通知 | 立即（<100ms） | 中等 |

## 总结

**问题已解决！** 通过SSE通知队头节点锁已被分配，队头节点可以立即重新请求锁，而不需要等待定期检查。

**关键点**：
1. ✅ `processQueue` 返回节点ID
2. ✅ `notifyLockAssigned` 通过SSE发送"锁已分配"事件
3. ✅ 客户端通过NodeID匹配识别"锁已分配"事件
4. ✅ 队头节点立即重新请求锁，获得锁后继续操作

