# 长连接和短连接客户端设计说明

## 概述

分布式锁客户端采用**双客户端设计**，分别使用 `ShortClient`（短连接）和 `LongClient`（长连接）来处理不同类型的HTTP请求。这种设计是为了满足不同场景下的超时需求。

---

## 设计结构

### 客户端定义

```go
type LockClient struct {
    ServerURL   string       // 锁服务端地址
    ShortClient *http.Client // 短连接客户端（用于普通HTTP请求，有超时）
    LongClient  *http.Client // 长连接客户端（用于SSE订阅和镜像操作，无超时）
    NodeID      string       // 当前节点ID
    
    // 重试配置
    MaxRetries     int           // 最大重试次数（默认3次）
    RetryInterval  time.Duration // 重试间隔（默认1秒）
    RequestTimeout time.Duration // 请求超时时间（默认30秒）
}
```

### 初始化代码

```go
func NewLockClient(serverURL, nodeID string) *LockClient {
    return &LockClient{
        ServerURL: serverURL,
        ShortClient: &http.Client{
            Timeout: 30 * time.Second, // 短连接设置超时
        },
        LongClient: &http.Client{
            // 长连接不设置超时，用于SSE订阅和镜像操作（下载时间可能很长）
        },
        NodeID:         nodeID,
        MaxRetries:     3,
        RetryInterval:  1 * time.Second,
        RequestTimeout: 30 * time.Second,
    }
}
```

---

## 两种客户端的区别

### ShortClient（短连接客户端）

**配置**：
- ✅ **有超时**：`Timeout: 30 * time.Second`
- ✅ **用于普通HTTP请求**：请求-响应模式

**使用场景**：
1. **获取锁请求** (`POST /lock`)
2. **释放锁请求** (`POST /unlock`)

**特点**：
- 请求时间短，通常在几毫秒到几秒内完成
- 需要超时保护，避免长时间等待
- 适合快速响应的操作

### LongClient（长连接客户端）

**配置**：
- ❌ **无超时**：不设置 `Timeout`
- ✅ **用于长连接**：保持连接直到主动关闭

**使用场景**：
1. **SSE订阅** (`GET /lock/subscribe`)
2. **镜像操作**（下载时间可能很长）

**特点**：
- 连接时间不确定，可能持续几分钟到几小时
- 不能设置超时，否则会中断正在进行的操作
- 适合需要长时间保持连接的场景

---

## 设计原因

### 1. 不同操作的超时需求不同

#### 问题场景

**场景A：普通HTTP请求（需要超时）**
```
节点A请求锁 → POST /lock → 服务端处理 → 返回响应
总耗时：通常 < 1秒
```

如果使用无超时的客户端：
- ❌ 网络故障时，请求会一直等待
- ❌ 服务端异常时，客户端无法及时感知
- ❌ 资源浪费，连接长时间占用

**场景B：SSE订阅（不能有超时）**
```
节点A订阅事件 → GET /lock/subscribe → 保持连接 → 等待事件推送
总耗时：不确定，可能几分钟到几小时
```

如果使用有超时的客户端（30秒）：
- ❌ 30秒后连接被强制关闭
- ❌ 无法接收到后续的事件推送
- ❌ 需要不断重连，效率低下

**场景C：镜像下载（不能有超时）**
```
节点A下载镜像层 → 开始下载 → 下载中... → 下载完成
总耗时：根据镜像大小，可能几分钟到几小时
```

如果使用有超时的客户端：
- ❌ 下载过程中连接被超时中断
- ❌ 下载失败，需要重新开始
- ❌ 浪费带宽和时间

### 2. 超时设置的冲突

**单一客户端的问题**：

```go
// 如果只使用一个客户端
client := &http.Client{
    Timeout: 30 * time.Second, // 必须设置超时
}

// 问题1：SSE订阅会被超时中断
resp, err := client.Do(sseRequest) // 30秒后自动关闭

// 问题2：镜像下载会被超时中断
resp, err := client.Do(downloadRequest) // 30秒后自动关闭
```

**解决方案：双客户端设计**：

```go
// 短连接：有超时，用于快速请求
ShortClient: &http.Client{
    Timeout: 30 * time.Second,
}

// 长连接：无超时，用于长时间操作
LongClient: &http.Client{
    // 不设置Timeout
}
```

---

## 使用场景详细分析

### 场景1：获取锁请求（使用 ShortClient）

```go
// tryLockOnce 函数
func (c *LockClient) tryLockOnce(ctx context.Context, request *Request) (*LockResult, error) {
    // 创建HTTP请求
    req, err := http.NewRequestWithContext(ctx, "POST", c.ServerURL+"/lock", bytes.NewBuffer(jsonData))
    
    // 使用短连接客户端，有超时保护
    resp, err := c.ShortClient.Do(req)
    // ...
}
```

**为什么使用 ShortClient**：
- ✅ 请求时间短（通常 < 1秒）
- ✅ 需要超时保护，避免网络故障时长时间等待
- ✅ 快速失败，及时重试

**超时处理**：
```go
if errors.Is(err, context.DeadlineExceeded) {
    return nil, fmt.Errorf("请求超时: %w", err)
}
```

### 场景2：SSE订阅（使用 LongClient）

```go
// waitForLock 函数
func (c *LockClient) waitForLock(ctx context.Context, request *Request) (*LockResult, error) {
    // 构建订阅 URL
    subscribeURL := fmt.Sprintf("%s/lock/subscribe?type=%s&resource_id=%s", ...)
    
    // 创建 SSE 订阅请求
    req, err := http.NewRequestWithContext(ctx, "GET", subscribeURL, nil)
    req.Header.Set("Accept", "text/event-stream")
    
    // 使用长连接客户端，无超时限制
    resp, err := c.LongClient.Do(req)
    // ...
    
    // 持续读取SSE流
    scanner := bufio.NewScanner(resp.Body)
    for scanner.Scan() {
        // 处理事件
    }
}
```

**为什么使用 LongClient**：
- ✅ 连接需要长时间保持（等待锁释放）
- ✅ 不能设置超时，否则会中断连接
- ✅ 通过 `context.Context` 控制取消，而不是超时

**取消机制**：
```go
select {
case <-ctx.Done():
    resp.Body.Close()
    return nil, ctx.Err()
default:
    // 继续处理
}
```

### 场景3：释放锁请求（使用 ShortClient）

```go
// tryUnlockOnce 函数
func (c *LockClient) tryUnlockOnce(ctx context.Context, request *Request) error {
    // 创建HTTP请求
    req, err := http.NewRequestWithContext(ctx, "POST", c.ServerURL+"/unlock", bytes.NewBuffer(jsonData))
    
    // 使用短连接客户端，有超时保护
    resp, err := c.ShortClient.Do(req)
    // ...
}
```

**为什么使用 ShortClient**：
- ✅ 请求时间短（通常 < 1秒）
- ✅ 需要超时保护
- ✅ 快速完成，释放资源

---

## 设计优势

### 1. 精确的超时控制

**短连接**：
- ✅ 快速请求有超时保护
- ✅ 网络故障时快速失败
- ✅ 避免资源浪费

**长连接**：
- ✅ 长时间操作不受超时限制
- ✅ 支持SSE实时推送
- ✅ 支持长时间下载

### 2. 职责分离

**ShortClient**：
- 职责：处理快速请求-响应操作
- 特点：有超时，快速失败

**LongClient**：
- 职责：处理长时间连接操作
- 特点：无超时，持续连接

### 3. 灵活性

- ✅ 可以根据不同场景选择合适的客户端
- ✅ 超时策略独立配置
- ✅ 易于扩展和维护

---

## 错误处理

### ShortClient 超时错误

```go
resp, err := c.ShortClient.Do(req)
if err != nil {
    // ShortClient.Timeout 触发时，err 会是 url.Error{Op: "Get", Err: context.DeadlineExceeded}
    if errors.Is(err, context.DeadlineExceeded) {
        return nil, fmt.Errorf("请求超时: %w", err)
    }
    if errors.Is(err, context.Canceled) {
        return nil, fmt.Errorf("请求被取消: %w", err)
    }
    return nil, fmt.Errorf("发送请求失败: %w", err)
}
```

**处理方式**：
- 超时错误：可以重试
- 取消错误：不重试
- 其他错误：根据错误类型决定是否重试

### LongClient 连接错误

```go
resp, err := c.LongClient.Do(req)
if err != nil {
    return nil, fmt.Errorf("订阅失败: %w", err)
}
```

**处理方式**：
- 连接失败：可以重新建立连接
- 通过 `context.Context` 控制取消
- 不依赖超时机制

---

## 实际使用示例

### 完整的锁获取流程

```go
// 1. 使用 ShortClient 快速请求锁
result, err := c.tryLockOnce(ctx, request)
// 如果获得锁，直接返回
if result.Acquired {
    return result, nil
}

// 2. 如果没有获得锁，使用 LongClient 订阅SSE事件
return c.waitForLock(ctx, request)
```

**流程说明**：
1. **第一步**：使用 `ShortClient` 快速请求锁（有超时保护）
2. **第二步**：如果未获得锁，使用 `LongClient` 订阅SSE事件（无超时限制）

### SSE订阅流程

```go
// 使用 LongClient 建立SSE连接
resp, err := c.LongClient.Do(req)

// 持续读取事件流
scanner := bufio.NewScanner(resp.Body)
for scanner.Scan() {
    // 处理SSE事件
    // 连接会一直保持，直到：
    // 1. 收到事件并处理完成
    // 2. context被取消
    // 3. 连接出错
}
```

---

## 配置建议

### 短连接超时时间

**当前配置**：`30 * time.Second`

**建议**：
- ✅ **开发环境**：可以设置较短（10-15秒）
- ✅ **生产环境**：根据网络情况调整（20-30秒）
- ✅ **高延迟网络**：可以适当增加（60秒）

### 长连接配置

**当前配置**：无超时

**建议**：
- ✅ **保持无超时**：SSE和下载操作需要长时间连接
- ✅ **使用 context 控制**：通过 `context.Context` 实现取消机制
- ✅ **连接池配置**：可以配置 `Transport` 的连接池参数

---

## 常见问题

### Q1: 为什么不能只使用一个客户端？

**A**: 因为不同操作的超时需求不同：
- 普通HTTP请求需要超时保护（避免长时间等待）
- SSE订阅和下载操作不能有超时（需要长时间保持连接）

### Q2: 长连接会不会导致资源泄漏？

**A**: 不会，因为：
- 使用 `context.Context` 控制取消
- 连接会在操作完成后主动关闭
- 错误时会及时关闭连接

### Q3: 短连接的超时时间可以调整吗？

**A**: 可以，通过修改 `ShortClient.Timeout` 配置：
```go
ShortClient: &http.Client{
    Timeout: 60 * time.Second, // 可以根据需要调整
}
```

### Q4: 长连接如何实现超时控制？

**A**: 通过 `context.Context` 实现：
```go
// 创建带超时的context
ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
defer cancel()

// 使用context控制请求
req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
```

---

## 总结

### 设计原则

1. **短连接**：快速请求，有超时保护
2. **长连接**：长时间操作，无超时限制
3. **职责分离**：不同场景使用不同客户端
4. **灵活控制**：通过 context 实现取消机制

### 关键点

- ✅ **ShortClient**：用于 `POST /lock` 和 `POST /unlock`，有30秒超时
- ✅ **LongClient**：用于 `GET /lock/subscribe` 和镜像操作，无超时
- ✅ **错误处理**：区分超时错误和取消错误
- ✅ **资源管理**：及时关闭连接，避免泄漏

这种设计既保证了快速请求的响应性，又支持了长时间操作的稳定性，是一个平衡性能和可靠性的优秀方案。

