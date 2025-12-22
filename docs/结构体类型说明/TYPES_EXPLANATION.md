# 结构体类型说明：LockInfo vs OperationEvent

## 概述

`LockInfo` 和 `OperationEvent` 是两个用途不同的结构体，虽然字段相似，但服务于不同的场景。

---

## 1. LockInfo - 服务端内部状态管理

### 用途
- **服务端内部使用**：存储在内存中，管理锁的完整生命周期
- **状态查询**：通过 `/lock/status` 接口查询锁的状态
- **轮询机制**：客户端通过轮询查询操作是否完成

### 字段说明

```go
type LockInfo struct {
    Request     *LockRequest  // 完整的请求信息（包含 NodeID、Timestamp 等）
    AcquiredAt  time.Time     // 锁获取时间（用于监控和调试）
    Completed   bool          // 操作是否完成（关键状态）
    Success     bool          // 操作是否成功（关键状态）
    CompletedAt time.Time     // 操作完成时间（用于监控和日志）
}
```

### 特点
- **持久性**：操作成功后，锁信息会保留一段时间（直到被清理）
- **完整性**：包含完整的请求上下文信息
- **状态跟踪**：用于跟踪锁的整个生命周期

### 使用场景

```go
// 1. 创建锁时
shard.locks[key] = &LockInfo{
    Request:    request,
    AcquiredAt: time.Now(),
    Completed:  false,
    Success:    false,
}

// 2. 操作完成时更新
lockInfo.Completed = true
lockInfo.Success = request.Success
lockInfo.CompletedAt = time.Now()

// 3. 客户端轮询查询
acquired, completed, success := lm.GetLockStatus(...)
```

---

## 2. OperationEvent - 事件通知（订阅者模式）

### 用途
- **实时通知**：通过 SSE (Server-Sent Events) 推送给订阅者
- **事件驱动**：客户端不需要轮询，实时收到操作完成的通知
- **轻量级**：只包含必要的事件信息，不包含完整的请求上下文

### 字段说明

```go
type OperationEvent struct {
    Type        string    // 操作类型（pull/update/delete）
    ResourceID  string    // 资源ID
    NodeID      string    // 执行操作的节点ID
    Success     bool      // 操作是否成功
    Error       string    // 错误信息（如果有）
    CompletedAt time.Time // 操作完成时间
}
```

### 特点
- **一次性**：事件发送后不再保留
- **轻量级**：只包含事件相关的信息
- **实时性**：立即推送给所有订阅者

### 使用场景

```go
// 操作完成时，创建事件并广播
event := &OperationEvent{
    Type:        request.Type,
    ResourceID:  request.ResourceID,
    NodeID:      request.NodeID,
    Success:     true,
    Error:       request.Error,
    CompletedAt: lockInfo.CompletedAt,
}
lm.broadcastEvent(shard, key, event)
```

---

## 3. 关键区别对比

| 特性 | LockInfo | OperationEvent |
|------|----------|----------------|
| **用途** | 服务端内部状态管理 | 事件通知（订阅者模式） |
| **存储位置** | 服务端内存（map） | 不存储（发送后丢弃） |
| **生命周期** | 从获取锁到清理 | 一次性事件 |
| **查询方式** | 轮询查询（主动） | SSE推送（被动） |
| **包含信息** | 完整的请求上下文 | 仅事件相关信息 |
| **持久性** | 保留到清理 | 不保留 |

---

## 4. 关于 Success 和 Error 字段

### 问题：它们不是完全互补的

**常见误解**：
- ❌ `success=false` 时，`error` 一定有值
- ❌ `success=true` 时，`error` 一定为空

**实际情况**：

```go
// 情况1：操作成功，无错误
Success: true
Error: ""

// 情况2：操作失败，有错误信息
Success: false
Error: "下载失败: 网络超时"

// 情况3：操作失败，但没有具体错误信息
Success: false
Error: ""  // 可能为空

// 情况4：操作成功，但有警告信息（理论上可能）
Success: true
Error: "警告: 文件已存在"  // 虽然成功，但可能有警告
```

### 设计原因

1. **灵活性**：允许操作失败但没有具体错误信息的情况
2. **扩展性**：未来可能支持警告信息（成功但有警告）
3. **兼容性**：不同场景下错误信息的可用性不同

### 推荐使用方式

```go
// 判断操作是否成功
if event.Success {
    // 操作成功
    if event.Error != "" {
        // 有警告信息（可选）
        log.Printf("警告: %s", event.Error)
    }
} else {
    // 操作失败
    if event.Error != "" {
        // 有错误信息
        log.Printf("错误: %s", event.Error)
    } else {
        // 没有具体错误信息
        log.Printf("操作失败（原因未知）")
    }
}
```

---

## 5. CompletedAt 字段的作用

### 在 LockInfo 中

**用途**：
1. **监控和调试**：记录操作完成的时间点
2. **性能分析**：计算操作耗时（`CompletedAt - AcquiredAt`）
3. **日志记录**：在日志中记录操作完成时间
4. **状态管理**：判断操作完成的时间顺序

**示例**：
```go
// 计算操作耗时
duration := lockInfo.CompletedAt.Sub(lockInfo.AcquiredAt)
log.Printf("操作耗时: %v", duration)

// 判断操作是否超时
if time.Since(lockInfo.CompletedAt) > timeout {
    // 清理过期的锁信息
}
```

### 在 OperationEvent 中

**用途**：
1. **客户端时间同步**：客户端知道操作何时完成
2. **事件排序**：多个事件可以按时间排序
3. **重试判断**：客户端可以根据时间判断是否需要重试
4. **监控和日志**：客户端可以记录事件接收时间

**示例**：
```go
// 客户端接收事件
event := receiveEvent()
log.Printf("收到操作完成事件: %s, 完成时间: %s", 
    event.ResourceID, event.CompletedAt)

// 判断事件是否过期
if time.Since(event.CompletedAt) > 5*time.Minute {
    log.Printf("事件已过期，忽略")
    return
}
```

---

## 6. 实际使用示例

### 场景：节点A操作完成，节点B等待

**流程1：使用 LockInfo（轮询模式）**

```go
// 节点B轮询查询
for {
    acquired, completed, success := client.GetLockStatus(...)
    if completed && success {
        // 操作已完成，跳过
        break
    }
    time.Sleep(500 * time.Millisecond)
}
```

**流程2：使用 OperationEvent（订阅模式）**

```go
// 节点B订阅事件
subscriber := client.Subscribe(resourceID)

// 等待事件
event := <-subscriber.Events()
if event.Success {
    // 操作成功，跳过
    log.Printf("操作在 %s 完成", event.CompletedAt)
}
```

---

## 7. 设计建议

### 当前设计是合理的

1. **职责分离**：
   - `LockInfo` 负责状态管理
   - `OperationEvent` 负责事件通知

2. **性能优化**：
   - `LockInfo` 保留完整信息，支持查询
   - `OperationEvent` 只包含必要信息，减少网络传输

3. **灵活性**：
   - 支持轮询和订阅两种模式
   - 客户端可以选择最适合的方式

### 可能的改进

1. **统一时间字段**：
   - 考虑添加 `AcquiredAt` 到 `OperationEvent`（如果需要）
   - 或者添加 `Duration` 字段（操作耗时）

2. **错误类型**：
   - 考虑使用枚举类型而不是字符串
   - 或者添加 `ErrorCode` 字段

3. **事件版本**：
   - 考虑添加版本号，便于未来扩展

---

## 总结

- **LockInfo**：服务端内部状态管理，支持轮询查询
- **OperationEvent**：事件通知，支持实时推送
- **Success 和 Error**：不是完全互补，提供灵活性
- **CompletedAt**：用于监控、日志和性能分析

两个结构体服务于不同的场景，设计是合理的。

