# Context 在分布式锁客户端中的详细解释

## 什么是 Context？

`context.Context` 是 Go 语言中用于**传递请求范围的值、取消信号和超时**的标准机制。

### Context 的核心功能

1. **取消信号（Cancellation）**：可以通知所有使用该 context 的 goroutine 停止工作
2. **超时控制（Timeout）**：可以设置操作的超时时间
3. **值传递（Value Passing）**：可以在 context 中传递键值对

## Context 在代码中的传递链

### 完整的调用链

```
用户代码
  ↓ ctx (可能是 context.Background() 或 context.WithTimeout(...))
Lock(ctx, request)
  ↓ ctx (传递原始 context)
tryLockOnce(ctx, request)
  ↓ ctx (传递原始 context)
  ├─→ reqCtx = context.WithTimeout(ctx, 30秒)  // 用于HTTP请求
  │   └─→ http.NewRequestWithContext(reqCtx, ...)
  │
  └─→ waitCtx = context.WithCancel(ctx)  // 用于SSE订阅
      └─→ waitForLock(waitCtx, request)
          └─→ http.NewRequestWithContext(waitCtx, ...)
```

## 详细流程分析

### 1. 入口：`Lock(ctx, request)`

**代码位置**：`client/client.go:43`

```go
func (c *LockClient) Lock(ctx context.Context, request *Request) (*LockResult, error) {
    // ctx 可能是：
    // - context.Background()：没有超时，不会自动取消
    // - context.WithTimeout(...)：有超时，超时后自动取消
    // - context.WithCancel(...)：可以手动取消
    
    // 检查 context 是否已被取消
    select {
    case <-ctx.Done():
        return nil, ctx.Err()  // 如果已取消，立即返回
    case <-time.After(c.RetryInterval):
        // 继续执行
    }
    
    // 调用 tryLockOnce，传递 ctx
    result, err := c.tryLockOnce(ctx, request)
    // ...
}
```

**作用**：
- 接收用户传入的 context
- 在重试循环中检查 context 是否被取消
- 如果 context 被取消，立即停止重试并返回

### 2. 单次尝试：`tryLockOnce(ctx, request)`

**代码位置**：`client/client.go:74`

```go
func (c *LockClient) tryLockOnce(ctx context.Context, request *Request) (*LockResult, error) {
    // 步骤1：创建带超时的 context（用于HTTP请求）
    reqCtx, cancel := context.WithTimeout(ctx, c.RequestTimeout)  // 30秒超时
    defer cancel()  // 函数返回时自动取消 reqCtx
    
    // 步骤2：使用 reqCtx 创建HTTP请求
    req, err := http.NewRequestWithContext(reqCtx, "POST", c.ServerURL+"/lock", ...)
    // reqCtx 的作用：
    // - 如果30秒内没有响应，reqCtx 会自动取消
    // - HTTP请求会检测到 reqCtx 被取消，停止等待
    
    resp, err := c.Client.Do(req)
    if err != nil {
        // 检查是否是超时
        if reqCtx.Err() == context.DeadlineExceeded {
            return nil, fmt.Errorf("请求超时: %w", err)
        }
        // ...
    }
    
    // 步骤3：如果没有获得锁，需要等待
    if !lockResp.Acquired {
        // 创建新的 context，取消超时限制，但保留取消功能
        waitCtx, cancel := context.WithCancel(ctx)
        defer cancel()
        return c.waitForLock(waitCtx, request)
    }
}
```

**关键点**：

1. **`reqCtx`（带超时的 context）**：
   - 用于 HTTP `/lock` 请求
   - 30秒超时，防止请求无限等待
   - 函数返回时自动取消（`defer cancel()`）

2. **`waitCtx`（可取消的 context）**：
   - 用于 SSE 订阅
   - **没有超时限制**，可以长时间等待
   - 但保留了取消功能（如果原始 `ctx` 被取消，`waitCtx` 也会被取消）

### 3. SSE 订阅：`waitForLock(ctx, request)`

**代码位置**：`client/client.go:149`

```go
func (c *LockClient) waitForLock(ctx context.Context, request *Request) (*LockResult, error) {
    for {  // 外层循环：如果连接断开，重新订阅
        // 检查 context 是否已被取消
        select {
        case <-ctx.Done():
            return nil, ctx.Err()  // 如果已取消，立即返回
        default:
            // 继续执行
        }
        
        // 创建 SSE 订阅请求
        req, err := http.NewRequestWithContext(ctx, "GET", subscribeURL, nil)
        // ctx 的作用：
        // - 如果 context 被取消，HTTP请求会立即停止
        // - SSE连接会立即断开
        
        // 使用没有超时的 Client
        sseClient := &http.Client{
            // 不设置Timeout，允许长时间保持连接
        }
        resp, err := sseClient.Do(req)
        
        // 读取 SSE 流
        scanner := bufio.NewScanner(resp.Body)
        for scanner.Scan() {
            // 在读取过程中，持续检查 context 是否被取消
            select {
            case <-ctx.Done():
                resp.Body.Close()
                return nil, ctx.Err()  // 如果已取消，立即停止读取
            default:
                // 继续读取
            }
            
            // 处理 SSE 事件
            // ...
        }
    }
}
```

**关键点**：

1. **持续检查 `ctx.Done()`**：
   - 在循环开始前检查
   - 在读取 SSE 流的过程中检查
   - 如果 context 被取消，立即停止并返回

2. **使用没有超时的 `sseClient`**：
   - 避免 HTTP Client 的30秒超时限制
   - 允许 SSE 连接长时间保持

## Context 的工作原理

### 1. Context 的继承关系

```
原始 ctx (可能是 context.Background() 或 context.WithTimeout(...))
  ↓
reqCtx = context.WithTimeout(ctx, 30秒)
  └─→ 继承原始 ctx 的取消功能 + 添加30秒超时
  
waitCtx = context.WithCancel(ctx)
  └─→ 继承原始 ctx 的取消功能，但不添加超时
```

### 2. Context 的取消传播

```
原始 ctx 被取消
  ↓
waitCtx 也会被取消（因为它是从原始 ctx 创建的）
  ↓
waitForLock 中的 `<-ctx.Done()` 会收到信号
  ↓
立即停止 SSE 订阅，返回错误
```

### 3. Context 的超时机制

**`context.WithTimeout`**：
```go
reqCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
// 30秒后，reqCtx 会自动取消
// 所有使用 reqCtx 的操作都会收到取消信号
```

**`context.WithCancel`**：
```go
waitCtx, cancel := context.WithCancel(ctx)
// waitCtx 不会自动取消
// 只有在以下情况才会取消：
// 1. 原始 ctx 被取消
// 2. 手动调用 cancel()
```

## 为什么需要修改 Context 处理？

### 问题场景

**修改前的代码**：
```go
// 问题：如果原始 ctx 有超时，SSE订阅会在超时后断开
return c.waitForLock(ctx, request)
```

**问题**：
- 如果用户传入的 `ctx` 有超时（例如60秒），SSE订阅会在60秒后断开
- 如果操作需要更长时间（例如90秒），SSE订阅会提前断开，无法收到完成事件

### 解决方案

**修改后的代码**：
```go
// 创建新的 context，取消超时限制，但保留取消功能
waitCtx, cancel := context.WithCancel(ctx)
defer cancel()
return c.waitForLock(waitCtx, request)
```

**优势**：
1. **取消超时限制**：`waitCtx` 没有超时，可以长时间等待
2. **保留取消功能**：如果原始 `ctx` 被取消，`waitCtx` 也会被取消
3. **资源清理**：`defer cancel()` 确保函数返回时清理资源

## Context 的关键方法

### 1. `ctx.Done()`

**作用**：返回一个 channel，当 context 被取消或超时时，这个 channel 会被关闭

**使用方式**：
```go
select {
case <-ctx.Done():
    // context 已被取消或超时
    return nil, ctx.Err()
default:
    // context 仍然有效，继续执行
}
```

### 2. `ctx.Err()`

**作用**：返回 context 被取消的原因

**返回值**：
- `nil`：context 仍然有效
- `context.Canceled`：context 被手动取消
- `context.DeadlineExceeded`：context 超时

### 3. `context.WithTimeout(ctx, duration)`

**作用**：创建一个带超时的 context

**特点**：
- 继承原始 context 的取消功能
- 添加超时限制
- 超时后自动取消

### 4. `context.WithCancel(ctx)`

**作用**：创建一个可取消的 context

**特点**：
- 继承原始 context 的取消功能
- **不添加超时限制**
- 可以手动取消（调用 `cancel()`）

## 完整的执行流程示例

### 场景：节点B等待节点A完成操作

```
T1: 用户调用 client.Lock(ctx, request)
    ctx = context.Background()  // 没有超时
    
T2: Lock() 调用 tryLockOnce(ctx, request)
    ctx = context.Background()
    
T3: tryLockOnce() 创建 reqCtx
    reqCtx = context.WithTimeout(ctx, 30秒)
    → 用于 HTTP /lock 请求
    
T4: HTTP /lock 请求返回 acquired=false
    → 锁被节点A占用
    
T5: tryLockOnce() 创建 waitCtx
    waitCtx = context.WithCancel(ctx)
    → 没有超时限制，可以长时间等待
    
T6: waitForLock(waitCtx, request) 开始
    → 建立 SSE 订阅连接
    
T7: 持续检查 waitCtx.Done()
    → 如果 waitCtx 被取消，立即停止
    
T8: 节点A完成操作，服务端广播事件
    → SSE 连接收到事件
    
T9: waitForLock() 处理事件，返回结果
    → 函数返回，defer cancel() 清理 waitCtx
```

### 场景：用户取消操作

```
T1: 用户调用 client.Lock(ctx, request)
    ctx = context.WithTimeout(context.Background(), 60秒)
    
T2: Lock() 调用 tryLockOnce(ctx, request)
    ctx = context.WithTimeout(..., 60秒)
    
T3: tryLockOnce() 创建 waitCtx
    waitCtx = context.WithCancel(ctx)
    → 继承原始 ctx 的取消功能
    
T4: waitForLock(waitCtx, request) 开始
    → 建立 SSE 订阅连接
    
T5: 60秒后，原始 ctx 超时
    → 原始 ctx 被取消
    
T6: waitCtx 也被取消（继承关系）
    → waitCtx.Done() 收到信号
    
T7: waitForLock() 检测到 waitCtx.Done()
    → 立即停止 SSE 订阅，返回错误
```

## 总结

### Context 的作用

1. **取消信号传递**：可以从上层传递取消信号到下层
2. **超时控制**：可以设置操作的超时时间
3. **资源清理**：可以通知所有相关操作停止并清理资源

### 在分布式锁客户端中的使用

1. **HTTP 请求**：使用 `context.WithTimeout`，设置30秒超时
2. **SSE 订阅**：使用 `context.WithCancel`，取消超时限制，但保留取消功能
3. **持续检查**：在循环和长时间操作中持续检查 `ctx.Done()`

### 关键设计决策

1. **为什么 SSE 订阅不使用超时？**
   - SSE 订阅需要长时间保持连接
   - 操作完成时间不确定，可能超过30秒
   - 使用 `context.WithCancel` 取消超时限制，但保留取消功能

2. **为什么保留取消功能？**
   - 如果用户取消操作，应该立即停止 SSE 订阅
   - 如果原始 context 被取消，waitCtx 也会被取消
   - 确保资源能够及时清理

3. **为什么使用 `defer cancel()`？**
   - 确保函数返回时清理 context 资源
   - 避免资源泄漏
   - 符合 Go 的最佳实践

