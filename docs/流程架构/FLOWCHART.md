# 流程与时序（适配 GitHub 展示，含详细实现）

> 当前代码：服务器只负责互斥与排队，单一 FIFO 队列/资源键；引用计数与“做/不做”在 Content 插件本地完成。下图中的 “subtype 队列” 为需求描述，可作为扩展位，现实现等效为单 FIFO。

## 1) 加锁流程（/lock）

```mermaid
flowchart TD
    A[客户端 /lock 请求<br/>Type, ResourceID, NodeID] --> B[校验参数]
    B -->|无效| B1[400 返回]
    B -->|有效| C[计算 key=Type:ResourceID<br/>定位 shard, 加 shard.mu]
    C --> D{锁是否存在?}
    D -->|存在且未完成| E[加入等待队列 FIFO<br/>return acquired=false, skip=false]
    D -->|存在且已完成| F[删除锁, 处理队列]
    F --> F1{队列有请求?}
    F1 -->|有| F2[分配队头为新锁<br/>return acquired=true]
    F1 -->|无| F3[return acquired=false]
    D -->|不存在| G[创建锁占位<br/>return acquired=true]
```

## 2) 解锁流程（/unlock）

```mermaid
flowchart TD
    A[客户端 /unlock<br/>Success, Err(optional)] --> B[校验参数]
    B -->|无效| B1[400 返回]
    B -->|有效| C[计算 key=Type:ResourceID<br/>定位 shard, 加 shard.mu]
    C --> D{锁存在且持有者?}
    D -->|否| D1[403 返回]
    D -->|是| E[删除锁，占位释放]
    E --> F[处理队列: 若有则分配队头为新锁]
    F --> G[返回 released=true/false]
```

## 3) 业务/客户端端到端时序（含 callback）

```mermaid
sequenceDiagram
    participant Content
    participant Client
    participant Server
    participant Callback as callback(内容侧)

    Content->>Client: Lock(ctx, req)
    Client->>Server: POST /lock (acquire)
    Server-->>Client: {acquired | queued | error}
    alt acquired
        Content->>Callback: ShouldSkipOperation/业务判断
        alt 需要执行
            Content->>Content: 执行业务操作
            Content->>Callback: UpdateRefCount/业务后处理
            Content->>Client: Unlock(ctx, success=true, err=nil)
        else 跳过/失败
            Content->>Client: Unlock(ctx, success=false, err=原因)
        end
        Client->>Server: POST /unlock (Success flag + Err)
        Server-->>Client: released
    else queued
        Client-->>Content: 等待/轮询（如需）
    else error
        Client-->>Content: 错误返回
    end
```

## 4) 连接监控（扩展位，当前可选）

```mermaid
flowchart TD
    A[服务器监控持锁请求连接] --> B{连接中断?}
    B -->|否| C[无操作]
    B -->|是| D{是否持有锁?}
    D -->|是| E[按失败路径释放锁: delete lock -> 处理队列]
    D -->|否| F[从等待队列移除该请求]
```

## 5) 说明

- 队列：当前实现为单 FIFO/资源键；若未来按 subtype 拆队列，可在“处理队列”处按 subtype 顺序挑选队头。  
- skip 字段：保留兼容，服务器始终返回 false；业务侧自行决定“做/不做”。  
- 引用计数：服务器不维护，内容侧通过 `callback` + 本地存储完成判断与更新。  
- 失败重试：客户端可在 /lock 层重试；若持锁连接异常，监控逻辑可触发失败释放并推进队列。  

---

## 6) 扩展版加锁（含 subtype 队列占位说明）

> 现实现为单 FIFO；下图给出按 subtype 拆队列的扩展位设计。

```mermaid
flowchart TD
    A[请求 /lock] --> B[校验参数]
    B -->|无效| X[400]
    B -->|有效| C[计算 key, 定位 shard, 加 shard.mu]
    C --> D{锁存在?}
    D -->|存在且未完成| E[按 subtype 放入对应等待队列]
    D -->|存在且已完成| F[删除锁, 依次扫描 subtype 队列分配队头]
    F --> F1{找到队头?}
    F1 -->|是| F2[分配锁, 返回 acquired=true]
    F1 -->|否| F3[返回 acquired=false]
    D -->|不存在| G[创建锁占位, 返回 acquired=true]
```

## 7) 引用计数管理流程（业务侧，Content + callback）

> 放在内容插件本地执行，服务器不参与。

```mermaid
flowchart TD
    A[业务决定执行某操作] --> B[callback.ShouldSkipOperation]
    B -->|skip=true| B1[直接跳过, 不请求锁]
    B -->|需要执行| C[请求分布式锁]
    C -->|acquired| D[执行业务]
    D --> E[callback.UpdateRefCount]
    E --> F[调用 /unlock]
    C -->|未获取| C1[等待/重试或返回]
```

## 8) 客户端重试机制（Lock）

```mermaid
flowchart TD
    A[Lock 调用] --> B[attempt=0..MaxRetries]
    B --> C[发送 /lock, 带超时]
    C --> D{响应}
    D -->|acquired/queued| E[返回结果]
    D -->|非重试类错误| F[返回错误]
    D -->|超时/网络错误| G{已达最大重试?}
    G -->|否| H[等待 RetryInterval]
    H --> B
    G -->|是| F
```

## 9) 分段锁并发处理

```mermaid
flowchart TD
    A[多个请求] --> B[计算 key]
    B --> C[FNV-1a 哈希]
    C --> D[keyHash % shardCount]
    D --> E{分段}
    E -->|不同分段| F[并发处理]
    E -->|同一分段| G[串行 + 队列]
```

## 10) 完整操作流程（Pull 示例，含 callback）

```mermaid
sequenceDiagram
    participant Content
    participant Callback
    participant Client
    participant Server

    Content->>Callback: ShouldSkipOperation(pull)
    alt skip
        Callback-->>Content: skip=true
        Content-->>Content: 不请求锁，直接返回
    else need
        Content->>Client: Lock()
        Client->>Server: POST /lock
        Server-->>Client: acquired / queued / error
        alt acquired
            Content->>Content: 下载/写入
            Content->>Callback: UpdateRefCount(+1)
            Content->>Client: Unlock(success=true)
        else queued
            Client-->>Content: 可轮询/等待
        else error
            Client-->>Content: 返回错误
        end
        Client->>Server: POST /unlock (Success flag)
        Server-->>Client: released
    end
```

## 11) HTTP 监控（详细）

```mermaid
flowchart TD
    A[监控 goroutine 绑定持锁请求] --> B{HTTP 连接中断?}
    B -->|否| Z[继续监控]
    B -->|是| C{该请求是否 holder?}
    C -->|是| D[按失败路径释放: delete lock -> processQueue]
    C -->|否| E[从等待队列移除该请求]
    D --> F[如有队头则分配锁并返回 acquired=true]
    E --> F
```

## 12) 端到端总流程（客户端请求 → 服务器处理 → 资源管理 → 释放）

```mermaid
flowchart TD
    %% 客户端侧
    A[Content 判断是否需要执行\ncallback.ShouldSkipOperation] -->|skip| A1[跳过业务\n不请求锁]
    A -->|需要执行| B[Client 发起 POST /lock (可带重试/超时)]
    B --> C{服务器校验参数}
    C -->|无效| C1[400 返回\n客户端处理错误]
    C -->|有效| D[计算 key=Type:ResourceID\n定位 shard, 加 shard.mu]

    %% 服务器 TryLock
    D --> E{锁是否存在?}
    E -->|存在且未完成| F[加入等待队列(FIFO/subtype 扩展位)\nreturn acquired=false]
    E -->|存在且已完成| G[删除锁, 扫描队列分配队头\n若无队头则返回 acquired=false]
    E -->|不存在| H[占位为当前锁\n返回 acquired=true]

    %% 客户端侧处理返回
    F --> I[可轮询/等待或直接返回未获取]
    G --> J[收到 acquired 或未获取]
    H --> K[收到 acquired=true]

    %% 业务执行 + callback
    K --> L[执行业务操作]
    L --> M[callback.UpdateRefCount/业务后处理]
    L --> N[POST /unlock (Success/Err)]
    J -->|未获取| I

    %% Unlock 服务器侧
    N --> O[计算 key, 定位 shard, 加 shard.mu]
    O --> P{锁存在且持有者?}
    P -->|否| P1[403 返回]
    P -->|是| Q[删除锁，处理队列]
    Q --> R{队列有请求?}
    R -->|有| S[分配队头为新锁\n返回 acquired=true 给排队请求]
    R -->|无| T[锁清理完成]

    %% 连接监控（并行）
    K -.-> U[监控 goroutine 绑定持锁连接]
    U --> V{连接中断?}
    V -->|是且 holder| W[按失败路径: delete lock -> processQueue]
    V -->|是且非 holder| X[从等待队列移除请求]
    V -->|否| Y[继续监控]
```
