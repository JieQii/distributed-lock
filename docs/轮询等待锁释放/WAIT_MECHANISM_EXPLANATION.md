# 等待队列中的节点如何发现操作已完成？

## 重要发现：有两个版本的客户端实现

### 版本1：`client/client.go` - SSE 订阅模式

**位置**：`client/client.go:144-248`

**机制**：使用 **SSE（Server-Sent Events）订阅**，不是轮询

**流程**：

```
1. 节点B请求锁 → 返回 acquired=false
2. 节点B进入 waitForLock()
3. 节点B建立 SSE 订阅连接：GET /lock/subscribe?type=pull&resource_id=xxx
4. 节点A操作完成，释放锁（成功）
   → 服务端保留锁信息：Completed=true, Success=true
   → 服务端广播事件：broadcastEvent()
5. 节点B通过 SSE 接收事件
   → 收到 OperationEvent{Success: true}
   → handleOperationEvent() 处理事件
   → 返回错误："其他节点已完成操作，请检查资源是否已存在"
```

**代码位置**：

```go
// client/client.go:144-248
func (c *LockClient) waitForLock(ctx context.Context, request *Request) (*LockResult, error) {
    // 建立 SSE 订阅
    subscribeURL := fmt.Sprintf("%s/lock/subscribe?type=%s&resource_id=%s", ...)
    // 读取 SSE 流
    scanner := bufio.NewScanner(resp.Body)
    // 处理事件
    c.handleOperationEvent(ctx, request, &event)
}

// client/client.go:271-278
if event.Success {
    return &LockResult{
        Acquired: false,
        Error:    fmt.Errorf("其他节点已完成操作，请检查资源是否已存在"),
    }, true, false
}
```

**特点**：
- ✅ **实时推送**：操作完成后立即收到事件
- ✅ **低延迟**：不需要轮询，事件立即推送
- ❌ **依赖 SSE 连接**：如果连接断开，需要重新订阅

### 版本2：`conchContent-v3/lockclient/client.go` - 轮询模式

**位置**：`conchContent-v3/lockclient/client.go:152-209`

**机制**：使用 **轮询 `/lock/status`**，每500ms查询一次

**流程**：

```
1. 节点B请求锁 → 返回 acquired=false
2. 节点B进入 waitForLock()
3. 节点B每500ms轮询一次：POST /lock/status
4. 节点A操作完成，释放锁（成功）
   → 服务端保留锁信息：Completed=true, Success=true
5. 节点B轮询 /lock/status
   → 返回：{acquired: false, completed: true, success: true}
   → 检查：if statusResp.Completed && statusResp.Success
   → 返回：Skipped=true，跳过操作
```

**代码位置**：

```go
// conchContent-v3/lockclient/client.go:152-209
func (c *LockClient) waitForLock(ctx context.Context, request *Request) (*LockResult, error) {
    ticker := time.NewTicker(500 * time.Millisecond) // 每500ms轮询一次
    for {
        case <-ticker.C:
            // 查询 /lock/status
            req, _ := http.NewRequestWithContext(ctx, "POST", c.ServerURL+"/lock/status", ...)
            resp, _ := c.Client.Do(req)
            
            var statusResp struct {
                Acquired  bool `json:"acquired"`
                Completed bool `json:"completed"`
                Success   bool `json:"success"`
            }
            
            // 如果操作已完成且成功，跳过操作
            if statusResp.Completed && statusResp.Success {
                return &LockResult{
                    Acquired: false,
                    Skipped:  true,
                }, nil
            }
    }
}
```

**特点**：
- ✅ **简单可靠**：不依赖长连接
- ✅ **自动重试**：轮询失败可以继续重试
- ❌ **有延迟**：最多延迟500ms才能发现操作已完成

## 服务端如何支持这两种机制？

### 操作成功时保留锁信息

**位置**：`server/lock_manager.go:153-169`

```go
if lockInfo.Success {
    // 操作成功：保留锁信息（标记为已完成），让队列中的节点通过轮询发现操作已完成
    // 不立即删除锁，也不分配锁给队列中的节点
    // 队列中的节点通过轮询 /lock/status 会发现 completed=true && success=true，从而跳过操作
    // 锁会在 TryLock 中被清理（当发现操作已完成时）
    log.Printf("[Unlock] 操作成功，保留锁信息: key=%s, node=%s, 等待队列中的节点通过轮询发现",
        key, request.NodeID)
    
    // 触发订阅消息广播
    lm.broadcastEvent(shard, key, &OperationEvent{...})
}
```

**设计目的**：
1. **支持轮询模式**：保留锁信息，让轮询 `/lock/status` 的节点发现 `completed=true && success=true`
2. **支持 SSE 模式**：广播事件，让订阅的节点立即收到 `Success=true` 的事件

### GetLockStatus 返回锁状态

**位置**：`server/lock_manager.go:190-209`

```go
func (lm *LockManager) GetLockStatus(lockType, resourceID, nodeID string) (bool, bool, bool) {
    lockInfo, exists := shard.locks[key]
    if !exists {
        return false, false, false // 没有锁
    }
    
    acquired := lockInfo.Request.NodeID == nodeID
    return acquired, lockInfo.Completed, lockInfo.Success
}
```

**用途**：
- 轮询模式的客户端通过 `/lock/status` 查询锁状态
- 返回 `completed=true, success=true` 时，客户端跳过操作

### broadcastEvent 广播事件

**位置**：`server/lock_manager.go:325-352`

```go
func (lm *LockManager) broadcastEvent(shard *resourceShard, key string, event *OperationEvent) {
    subscribers, exists := shard.subscribers[key]
    if !exists || len(subscribers) == 0 {
        return
    }
    
    // 发送事件给所有订阅者
    for _, sub := range subscribers {
        sub.SendEvent(event)  // SSE 推送
    }
}
```

**用途**：
- SSE 模式的客户端通过订阅接收事件
- 操作完成后立即推送 `Success=true` 的事件

## 两种机制的对比

| 特性 | SSE 订阅模式 | 轮询模式 |
|------|-------------|---------|
| **实现位置** | `client/client.go` | `conchContent-v3/lockclient/client.go` |
| **等待方式** | SSE 长连接 | 每500ms轮询 `/lock/status` |
| **延迟** | 实时（立即推送） | 最多500ms延迟 |
| **可靠性** | 依赖长连接 | 不依赖长连接 |
| **资源消耗** | 长连接占用资源 | 定期请求占用资源 |
| **发现机制** | 通过 SSE 事件 | 通过查询锁状态 |

## 当前问题分析

### 问题：为什么会有"操作已完成且成功"的锁记录？

**原因**：服务端设计如此，目的是同时支持两种机制

1. **支持轮询模式**：
   - 保留锁信息，让轮询 `/lock/status` 的节点发现操作已完成
   - 轮询节点查询到 `completed=true && success=true` 时跳过操作

2. **支持 SSE 模式**：
   - 广播事件，让订阅的节点立即收到操作完成的通知
   - SSE 节点收到 `Success=true` 的事件时处理

### 问题：新请求无法跳过操作

**原因**：TryLock 没有返回 `skip=true`

**当前代码**（`server/lock_manager.go:79-93`）：

```go
if lockInfo.Completed {
    if lockInfo.Success {
        log.Printf("[TryLock] 操作已完成且成功: key=%s, 清理锁", key)
    }
    delete(shard.locks, key)
    return false, false, ""  // ❌ 应该返回 skip=true
}
```

**解决方案**：修改 TryLock，返回 `skip=true`

```go
if lockInfo.Completed {
    if lockInfo.Success {
        log.Printf("[TryLock] 操作已完成且成功: key=%s, 清理锁", key)
        delete(shard.locks, key)
        return false, true, ""  // ✅ 返回 skip=true
    }
    // ...
}
```

## 总结

### 等待队列中的节点如何发现操作已完成？

**答案**：取决于使用的客户端版本

1. **SSE 订阅模式**（`client/client.go`）：
   - 通过 SSE 订阅接收事件
   - 操作完成后，服务端广播 `Success=true` 的事件
   - 客户端收到事件后，返回错误提示检查资源

2. **轮询模式**（`conchContent-v3/lockclient/client.go`）：
   - 每500ms轮询一次 `/lock/status`
   - 操作完成后，服务端保留锁信息（`Completed=true, Success=true`）
   - 客户端查询到 `completed=true && success=true` 时跳过操作

### 服务端的设计

服务端同时支持两种机制：
- ✅ **保留锁信息**：支持轮询模式查询锁状态
- ✅ **广播事件**：支持 SSE 模式接收事件推送

### 当前问题

新请求到来时，TryLock 发现锁已完成，但：
- ❌ 没有返回 `skip=true`
- ❌ 客户端无法知道应该跳过操作
- ❌ 客户端进入等待流程，但锁已经被清理了

**解决方案**：修改 TryLock，当发现操作已完成且成功时，返回 `skip=true`

