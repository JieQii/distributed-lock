# 客户端锁获取完整流程说明

## 整体流程概览

```
contentv2.Store.Writer()
    ↓
client.ClusterLock() 
    ↓
LockClient.Lock() [最多重试3次]
    ↓
tryLockOnce() [单次尝试]
    ├─ 获得锁 (acquired=true) → 直接返回 ✅
    ├─ 有错误 → 返回错误 ❌
    └─ 未获得锁 → waitForLock() [SSE订阅等待]
         ├─ 建立 SSE 订阅连接
         ├─ 等待服务端推送事件
         └─ 处理事件并返回结果
```

## 详细流程说明

### 阶段1: contentv2 发起锁请求

**位置**: `contentv2/store.go:Writer()`

```go
req := &client.Request{
    Type:       client.OperationTypePull,
    ResourceID: resourceID,  // digest字符串
    NodeID:     s.nodeID,
}
result, err := client.ClusterLock(ctx, s.lockClient, req)
```

### 阶段2: 客户端重试机制

**位置**: `client/client.go:Lock()`

```go
func (c *LockClient) Lock(ctx context.Context, request *Request) (*LockResult, error) {
    // 最多重试3次
    for attempt := 0; attempt <= c.MaxRetries; attempt++ {
        result, err := c.tryLockOnce(ctx, request)
        if err == nil {
            return result, nil  // 成功则返回
        }
        // 判断是否应该重试（网络错误等）
        if !c.shouldRetry(err) {
            return nil, err
        }
    }
    return nil, fmt.Errorf("获取锁失败，已重试%d次", c.MaxRetries)
}
```

**重试条件**：
- 网络错误（timeout、connection、network、EOF、refused）
- 非业务逻辑错误（如服务端返回 500）

### 阶段3: 单次尝试获取锁

**位置**: `client/client.go:tryLockOnce()`

#### 3.1 发送请求到服务端

```go
POST /lock
Body: {
    "type": "pull",
    "resource_id": "sha256:xxx",
    "node_id": "node-1"
}
```

#### 3.2 处理服务端响应

服务端可能返回以下几种情况：

##### ✅ 情况1: 直接获得锁 (`acquired=true`)

```json
{
    "acquired": true,
    "message": "获得锁"
}
```

**处理**：
```go
if lockResp.Acquired {
    return &LockResult{
        Acquired: true,
    }, nil
}
```

**返回给 contentv2**：`result.Acquired = true`，可以开始写入操作

##### ❌ 情况2: 有错误

```json
{
    "acquired": false,
    "error": "引用计数不为0"
}
```

**处理**：
```go
if lockResp.Error != "" {
    return &LockResult{
        Acquired: false,
        Error:    fmt.Errorf("%s", lockResp.Error),
    }, nil
}
```

**返回给 contentv2**：返回错误，不进行重试

##### ⏳ 情况3: 未获得锁 (`acquired=false`)

```json
{
    "acquired": false,
    "message": "锁已被占用"
}
```

**处理**：
```go
// 如果没有获得锁，需要等待
return c.waitForLock(ctx, request)
```

**说明**：此时锁被其他节点持有，需要进入等待流程

### 阶段4: SSE 订阅等待（核心流程）

**位置**: `client/client.go:waitForLock()`

#### 4.1 建立 SSE 订阅连接

```go
// 构建订阅 URL
subscribeURL := fmt.Sprintf("%s/lock/subscribe?type=%s&resource_id=%s",
    c.ServerURL,
    url.QueryEscape(request.Type),
    url.QueryEscape(request.ResourceID))

// 创建 SSE 订阅请求
GET /lock/subscribe?type=pull&resource_id=sha256:xxx
Headers:
    Accept: text/event-stream
    Cache-Control: no-cache
```

**服务端行为**：
1. 将当前客户端加入订阅者列表
2. 如果锁已存在，客户端会被加入等待队列（FIFO）
3. 建立长连接，等待事件推送

#### 4.2 等待服务端推送事件

**SSE 事件格式**：
```
data: {"type":"pull","resource_id":"sha256:xxx","node_id":"node-2","success":true,"completed_at":"..."}

```

#### 4.3 处理收到的事件

**位置**: `client/client.go:handleOperationEvent()`

##### 📢 事件类型1: 操作成功 (`success=true`)

**场景**：获得锁的节点操作成功完成

**服务端行为**：
1. 清理锁信息（标记为已完成且成功）
2. 广播事件给所有订阅者
3. 等待队列中的节点收到事件后，需要检查资源是否已存在

**客户端处理**：
```go
if event.Success {
    // 操作成功，但当前节点没有获得锁
    // 上层应该检查资源是否已存在，如果存在就不需要操作
    return &LockResult{
        Acquired: false,
        Error:    fmt.Errorf("其他节点已完成操作，请检查资源是否已存在"),
    }, true, false
}
```

**返回给 contentv2**：返回错误，提示上层检查资源是否已存在
**注意**：上层（containerd）在调用 `Writer()` 之前已经检查过资源是否存在，如果资源已存在就不会调用 `Writer()`。这里返回错误是为了处理并发场景：其他节点在检查之后完成了下载。

##### 🔄 事件类型2: 操作失败 (`success=false`)

**场景**：获得锁的节点操作失败

**服务端行为**：
1. 删除锁
2. 通过 `processQueue()` 将锁分配给等待队列中的第一个节点（FIFO）
3. 广播操作失败事件给所有订阅者

**客户端处理**：
```go
// 收到失败事件后，再次尝试获取锁
POST /lock
Body: {
    "type": "pull",
    "resource_id": "sha256:xxx",
    "node_id": "node-1"
}
```

**可能的结果**：

- **当前节点是队列第一个**：
  ```json
  {
      "acquired": true
  }
  ```
  返回 `result.Acquired = true`，可以开始操作 ✅

- **当前节点不是队列第一个**：
  ```json
  {
      "acquired": false
  }
  ```
  返回 `needResubscribe = true`，重新建立 SSE 订阅继续等待 ⏳

##### ❌ 事件类型3: 有错误信息

```go
if event.Error != "" {
    return &LockResult{
        Acquired: false,
        Error:    fmt.Errorf("%s", event.Error),
    }, true, false
}
```

### 阶段5: contentv2 处理结果

**位置**: `contentv2/store.go:Writer()`

```go
// 检查是否有错误
if result.Error != nil {
    return nil, fmt.Errorf("distributed lock error: %w", result.Error)
}

// 如果获得锁，创建 writer
if result.Acquired {
    w, err := s.writeStore.Writer(ctx, opts...)
    if err != nil {
        req.Error = err.Error()
        _ = client.ClusterUnLock(ctx, s.lockClient, req)
        return nil, err
    }
    return &distributedWriter{
        writer:     w,
        lockClient: s.lockClient,
        request:    req,  // 保存请求，用于后续解锁
        digest:     dgst,
    }, nil
}

// 理论上不应该到达这里，因为 waitForLock 会一直等待直到获得锁
return nil, fmt.Errorf("unexpected lock result: acquired=%v", result.Acquired)
```

## 关键点总结

### 1. 重试机制
- **网络层重试**：`Lock()` 方法最多重试3次，处理网络错误
- **业务层等待**：`waitForLock()` 通过 SSE 订阅等待业务事件

### 2. 两种返回结果
- **`Acquired=true`**：获得锁，可以开始操作
- **`Error!=nil`**：有错误，不进行重试
  - 业务错误（如引用计数不为0）
  - 其他节点已完成操作（提示上层检查资源是否已存在）

### 3. SSE 订阅的作用
- **实时通知**：避免轮询，减少服务端压力
- **事件驱动**：操作完成时立即通知等待的节点
- **自动重试**：操作失败时，队列中的第一个节点自动获得锁

### 4. 队列机制（FIFO）
- 未获得锁的节点按请求顺序加入队列
- 操作失败时，队列中的第一个节点自动获得锁
- 操作成功时，队列中的所有节点收到事件，需要检查资源是否已存在

## 时序图示例

### 场景1: 节点1获得锁，操作成功

```
节点1: POST /lock → acquired=true → 开始操作
节点2: POST /lock → acquired=false → 订阅等待
节点3: POST /lock → acquired=false → 订阅等待

节点1: 操作完成 → POST /unlock (success=true)
服务端: 
  1. 清理锁
  2. 广播事件 (success=true) → 所有订阅者
节点2: 收到事件 → Error="其他节点已完成操作" → 上层检查资源是否已存在
节点3: 收到事件 → Error="其他节点已完成操作" → 上层检查资源是否已存在

注意：上层（containerd）在调用 Writer() 之前已经检查过资源是否存在，
如果资源已存在就不会调用 Writer()。这里返回错误是为了处理并发场景。
```

### 场景2: 节点1获得锁，操作失败

```
节点1: POST /lock → acquired=true → 开始操作
节点2: POST /lock → acquired=false → 订阅等待（队列第1个）
节点3: POST /lock → acquired=false → 订阅等待（队列第2个）

节点1: 操作失败 → POST /unlock (success=false)
服务端: 
  1. 删除锁
  2. processQueue() → 分配锁给节点2
  3. 广播事件 (success=false) → 所有订阅者

节点2: 收到事件 → 再次 POST /lock → acquired=true → 开始操作 ✅
节点3: 收到事件 → 再次 POST /lock → acquired=false → 重新订阅等待 ⏳

注意：操作成功时，服务端会清理锁，不会分配锁给队列中的节点。
队列中的节点收到成功事件后，需要检查资源是否已存在。
```

