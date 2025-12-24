# 为什么操作失败需要广播事件？

## 用户的问题

用户提出了一个很好的问题：
- **操作成功需要广播**：因为队列里的其他节点可以不用去下载了（资源已存在）
- **操作失败不需要广播**：因为队列里的节点还是需要等待，只需要让队头的节点得到锁就行了

## 当前实现分析

### 操作失败时的流程

```
T1: 节点A操作失败，调用Unlock
    → 服务端删除锁：delete(shard.locks, key)
    → 服务端调用processQueue：分配锁给队头节点（节点B）
    → 服务端广播失败事件：broadcastEvent(...)
    
T2: 所有订阅者（包括节点B和其他节点）收到失败事件
    → handleOperationEvent处理失败事件
    → 重新调用 /lock 接口
    
T3: 节点B重新调用 /lock
    → TryLock检查锁是否存在
    → 如果锁已经被processQueue分配，返回true（节点B获得锁）
    
T4: 其他节点重新调用 /lock
    → TryLock检查锁是否存在
    → 如果锁已经被processQueue分配给节点B，返回false（其他节点继续等待）
```

### 问题分析

**用户说得对！** 如果 `processQueue` 已经将锁分配给了队头节点，那么：

1. **队头节点（节点B）**：
   - 锁已经被分配给它了
   - 它可以通过重新请求锁来获得锁（因为锁已经被分配给它了）
   - **不需要通过广播事件来通知**

2. **其他节点**：
   - 它们还在队列中等待
   - 它们不需要知道操作失败
   - **不需要通过广播事件来通知**

## 为什么当前实现需要广播事件？

### 当前实现的问题

当前实现中，客户端是通过 **SSE 订阅**来等待的：

```go
// client/client.go:149
func (c *LockClient) waitForLock(ctx context.Context, request *Request) (*LockResult, error) {
    // 建立 SSE 订阅连接
    // 等待事件...
}
```

**问题**：
- 如果操作失败时不广播事件，队头节点如何知道可以重新请求锁？
- 队头节点会一直等待在 SSE 订阅中，无法知道锁已经被分配给它了

### 当前实现的逻辑

1. **操作失败时广播事件**：
   - 所有订阅者收到失败事件
   - 所有订阅者重新调用 `/lock`
   - 队头节点获得锁，其他节点继续等待

2. **问题**：
   - 其他节点不需要知道操作失败
   - 广播事件给所有订阅者是多余的

## 更好的方案

### 方案1：操作失败时不广播事件，队头节点通过轮询发现锁已被分配

**实现**：
```go
// 操作失败时
delete(shard.locks, key)
lm.processQueue(shard, key)  // 分配锁给队头节点
// 不广播事件

// 队头节点通过轮询或重新请求锁来发现锁已被分配
```

**问题**：
- 队头节点需要轮询或重新请求锁
- 如果使用 SSE 订阅，队头节点会一直等待，无法知道锁已经被分配

### 方案2：操作失败时只通知队头节点

**实现**：
```go
// 操作失败时
delete(shard.locks, key)
nextRequest := lm.processQueue(shard, key)  // 分配锁给队头节点
// 只通知队头节点
lm.notifyNode(shard, key, nextRequest.NodeID, event)
```

**问题**：
- 需要知道哪个节点是队头节点
- 需要单独通知队头节点，而不是广播

### 方案3：操作失败时不广播事件，队头节点通过重新请求锁来获得锁

**实现**：
```go
// 操作失败时
delete(shard.locks, key)
lm.processQueue(shard, key)  // 分配锁给队头节点
// 不广播事件

// 队头节点通过重新请求锁来获得锁
// 因为锁已经被分配给它了，TryLock会返回true
```

**问题**：
- 如果队头节点使用 SSE 订阅等待，它不会主动重新请求锁
- 需要修改客户端逻辑，让队头节点定期重新请求锁

## 最佳方案：操作失败时不广播事件

### 实现方案

**服务端修改**：
```go
} else {
    // 操作失败：删除锁并分配锁给队列中的下一个节点，让它继续尝试
    log.Printf("[Unlock] 操作失败，唤醒队列: key=%s, node=%s", key, request.NodeID)
    delete(shard.locks, key)
    lm.processQueue(shard, key)  // 分配锁给队头节点
    
    // 不广播事件，因为：
    // 1. 队头节点可以通过重新请求锁来获得锁（锁已经被分配给它了）
    // 2. 其他节点不需要知道操作失败，它们还在队列中等待
}
```

**客户端修改**：
```go
// waitForLock 中，如果SSE连接断开，重新请求锁
// 因为锁可能已经被processQueue分配了
func (c *LockClient) waitForLock(ctx context.Context, request *Request) (*LockResult, error) {
    for {
        // 建立 SSE 订阅连接
        // ...
        
        // 如果连接断开，重新请求锁
        // 因为锁可能已经被processQueue分配了
        result, err := c.tryLockOnce(ctx, request)
        if err == nil && result.Acquired {
            return result, nil
        }
        
        // 继续 SSE 订阅...
    }
}
```

### 优势

1. **简化逻辑**：操作失败时不广播事件，减少不必要的通信
2. **减少延迟**：其他节点不需要处理失败事件
3. **更清晰**：队头节点通过重新请求锁来获得锁，逻辑更清晰

### 问题

1. **队头节点如何知道可以重新请求锁？**
   - 如果使用 SSE 订阅，队头节点会一直等待
   - 需要修改客户端逻辑，让队头节点定期重新请求锁

2. **SSE 连接断开时如何处理？**
   - 如果 SSE 连接断开，客户端应该重新请求锁
   - 因为锁可能已经被 processQueue 分配了

## 建议的解决方案

### 方案：操作失败时不广播事件，队头节点通过重新请求锁来获得锁

**服务端修改**：
```go
} else {
    // 操作失败：删除锁并分配锁给队列中的下一个节点，让它继续尝试
    log.Printf("[Unlock] 操作失败，唤醒队列: key=%s, node=%s", key, request.NodeID)
    delete(shard.locks, key)
    lm.processQueue(shard, key)  // 分配锁给队头节点
    
    // 不广播事件，因为：
    // 1. 队头节点可以通过重新请求锁来获得锁（锁已经被分配给它了）
    // 2. 其他节点不需要知道操作失败，它们还在队列中等待
}
```

**客户端修改**：
```go
// waitForLock 中，定期重新请求锁
// 因为锁可能已经被processQueue分配了
func (c *LockClient) waitForLock(ctx context.Context, request *Request) (*LockResult, error) {
    ticker := time.NewTicker(1 * time.Second)  // 每秒检查一次
    defer ticker.Stop()
    
    for {
        select {
        case <-ctx.Done():
            return nil, ctx.Err()
        case <-ticker.C:
            // 定期重新请求锁，因为锁可能已经被processQueue分配了
            result, err := c.tryLockOnce(ctx, request)
            if err == nil && result.Acquired {
                return result, nil
            }
        default:
            // 建立 SSE 订阅连接
            // ...
        }
    }
}
```

## 结论

**用户说得对！** 操作失败时不需要广播事件，因为：

1. **队头节点**：锁已经被分配给它了，可以通过重新请求锁来获得锁
2. **其他节点**：不需要知道操作失败，它们还在队列中等待

**建议**：修改实现，操作失败时不广播事件，队头节点通过重新请求锁来获得锁。

