# 长连接和短连接客户端设计 - 可视化说明

## 架构图

```mermaid
graph TB
    subgraph Client["LockClient"]
        SC[ShortClient<br/>有超时: 30秒<br/>用于快速请求]
        LC[LongClient<br/>无超时<br/>用于长连接]
    end

    subgraph ShortOps["短连接操作"]
        Lock[POST /lock<br/>获取锁]
        Unlock[POST /unlock<br/>释放锁]
    end

    subgraph LongOps["长连接操作"]
        SSE[GET /lock/subscribe<br/>SSE订阅]
        Download[镜像下载<br/>长时间操作]
    end

    SC --> Lock
    SC --> Unlock
    LC --> SSE
    LC --> Download
```

## 使用场景对比

```mermaid
sequenceDiagram
    participant Node as 节点
    participant SC as ShortClient
    participant LC as LongClient
    participant Server as 服务端

    Note over Node,Server: 场景1: 获取锁（短连接）
    Node->>SC: POST /lock
    SC->>Server: 请求锁（30秒超时）
    Server-->>SC: 返回结果
    SC-->>Node: LockResult

    Note over Node,Server: 场景2: SSE订阅（长连接）
    Node->>LC: GET /lock/subscribe
    LC->>Server: 建立SSE连接（无超时）
    Server-->>LC: 推送事件1
    Server-->>LC: 推送事件2
    Server-->>LC: 推送事件N
    Note over LC: 连接持续保持，直到收到事件或取消
```

## 超时机制对比

```mermaid
graph LR
    subgraph Short["ShortClient (有超时)"]
        S1[请求开始] --> S2{30秒内<br/>收到响应?}
        S2 -->|是| S3[成功]
        S2 -->|否| S4[超时错误]
        S4 --> S5[快速失败]
    end

    subgraph Long["LongClient (无超时)"]
        L1[连接建立] --> L2[持续保持连接]
        L2 --> L3{收到事件或<br/>context取消?}
        L3 -->|收到事件| L4[处理事件]
        L3 -->|context取消| L5[关闭连接]
        L4 --> L2
    end
```

## 设计原因

```mermaid
flowchart TD
    Start[需要HTTP客户端] --> Decision{操作类型?}
    
    Decision -->|快速请求<br/>POST /lock<br/>POST /unlock| Short[ShortClient<br/>有超时: 30秒]
    Decision -->|长连接<br/>SSE订阅<br/>镜像下载| Long[LongClient<br/>无超时]
    
    Short --> ShortReason[原因:<br/>1. 请求时间短<br/>2. 需要超时保护<br/>3. 快速失败]
    
    Long --> LongReason[原因:<br/>1. 连接时间不确定<br/>2. 不能设置超时<br/>3. 需要持续连接]
    
    ShortReason --> ShortBenefit[优势:<br/>避免长时间等待<br/>及时感知错误]
    LongReason --> LongBenefit[优势:<br/>支持长时间操作<br/>实时事件推送]
```

## 错误处理流程

```mermaid
sequenceDiagram
    participant Client
    participant SC as ShortClient
    participant LC as LongClient
    participant Server

    Note over Client,Server: ShortClient 错误处理
    Client->>SC: POST /lock
    SC->>Server: 请求
    alt 30秒内响应
        Server-->>SC: 响应
        SC-->>Client: 成功
    else 30秒超时
        SC-->>Client: 超时错误
        Client->>Client: 重试
    else 网络错误
        SC-->>Client: 网络错误
        Client->>Client: 重试
    end

    Note over Client,Server: LongClient 错误处理
    Client->>LC: GET /lock/subscribe
    LC->>Server: 建立连接
    alt 连接成功
        Server-->>LC: 推送事件
        LC-->>Client: 处理事件
    else 连接失败
        LC-->>Client: 连接错误
        Client->>Client: 重新连接
    else context取消
        LC-->>Client: 取消
        Client->>Client: 停止订阅
    end
```

## 时间线对比

```mermaid
gantt
    title 短连接 vs 长连接时间线
    dateFormat X
    axisFormat %s秒

    section ShortClient
    请求锁          :0, 1s
    等待响应        :1s, 0.5s
    处理响应        :1.5s, 0.5s

    section LongClient
    建立SSE连接     :0, 0.5s
    等待事件1       :0.5s, 10s
    处理事件1       :10.5s, 0.5s
    等待事件2       :11s, 20s
    处理事件2       :31s, 0.5s
    持续连接        :31.5s, 100s
```

## 配置对比表

| 特性 | ShortClient | LongClient |
|------|-------------|------------|
| **超时设置** | ✅ 30秒 | ❌ 无超时 |
| **使用场景** | 快速请求-响应 | 长时间连接 |
| **典型操作** | POST /lock, POST /unlock | GET /lock/subscribe, 镜像下载 |
| **响应时间** | < 1秒 | 不确定（几分钟到几小时） |
| **错误处理** | 超时错误、网络错误 | 连接错误、context取消 |
| **重试机制** | ✅ 支持 | ✅ 支持（重新连接） |
| **资源占用** | 短时间占用 | 长时间占用 |

## 设计优势总结

```mermaid
mindmap
  root((双客户端设计))
    精确控制
      短连接有超时
      长连接无超时
      不同场景不同策略
    职责分离
      ShortClient: 快速请求
      LongClient: 长时间操作
      清晰的职责边界
    灵活性
      独立配置
      易于扩展
      易于维护
    可靠性
      快速失败
      避免资源浪费
      支持长时间操作
```

## 代码示例

### ShortClient 使用

```go
// 获取锁请求
func (c *LockClient) tryLockOnce(ctx context.Context, request *Request) (*LockResult, error) {
    req, err := http.NewRequestWithContext(ctx, "POST", c.ServerURL+"/lock", bytes.NewBuffer(jsonData))
    
    // 使用 ShortClient，有30秒超时
    resp, err := c.ShortClient.Do(req)
    if err != nil {
        if errors.Is(err, context.DeadlineExceeded) {
            return nil, fmt.Errorf("请求超时: %w", err)
        }
        return nil, fmt.Errorf("发送请求失败: %w", err)
    }
    // ...
}
```

### LongClient 使用

```go
// SSE订阅
func (c *LockClient) waitForLock(ctx context.Context, request *Request) (*LockResult, error) {
    req, err := http.NewRequestWithContext(ctx, "GET", subscribeURL, nil)
    req.Header.Set("Accept", "text/event-stream")
    
    // 使用 LongClient，无超时限制
    resp, err := c.LongClient.Do(req)
    if err != nil {
        return nil, fmt.Errorf("订阅失败: %w", err)
    }
    
    // 持续读取SSE流
    scanner := bufio.NewScanner(resp.Body)
    for scanner.Scan() {
        select {
        case <-ctx.Done():
            resp.Body.Close()
            return nil, ctx.Err()
        default:
            // 处理事件
        }
    }
}
```

## 关键设计决策

### 1. 为什么需要两个客户端？

**原因**：不同操作的超时需求不同
- 快速请求需要超时保护
- 长时间操作不能有超时

### 2. 为什么不使用 context.WithTimeout？

**原因**：`http.Client.Timeout` 和 `context.WithTimeout` 的作用不同
- `http.Client.Timeout`：控制整个请求的超时（包括连接、传输等）
- `context.WithTimeout`：可以控制，但需要手动管理

### 3. 长连接如何控制取消？

**方式**：使用 `context.Context`
- 通过 `context.Done()` 检测取消
- 不依赖超时机制
- 更灵活的控制方式

## 最佳实践

1. ✅ **短连接**：用于所有快速请求-响应操作
2. ✅ **长连接**：用于所有需要长时间保持连接的操作
3. ✅ **错误处理**：区分超时错误和取消错误
4. ✅ **资源管理**：及时关闭连接，避免泄漏
5. ✅ **重试机制**：根据错误类型决定是否重试

