# 分布式锁系统架构文档

> 本文档使用 Mermaid 图表，可在 GitHub、VS Code、Markdown 预览等工具中直接可视化。

## 系统架构概览

### 整体架构图

```mermaid
graph TB
    subgraph Content["Content 插件层"]
        Writer[Writer<br/>- OpenWriter<br/>- Write<br/>- Commit<br/>- Close]
        RefCountMgr[RefCountManager<br/>callback包<br/>- ShouldSkipOperation<br/>- UpdateRefCount]
        RefCountStorage[RefCountStorage<br/>本地存储<br/>- 内存/文件/DB]
    end

    subgraph Client["Client 包"]
        LockClient[LockClient<br/>- Lock<br/>- Unlock<br/>- ShortClient<br/>- LongClient]
    end

    subgraph Server["Server 端"]
        Handler[Handler<br/>- Lock<br/>- Unlock<br/>- Subscribe]
        LockManager[LockManager<br/>- TryLock<br/>- Unlock<br/>- 32个分段锁]
        Shard[resourceShard<br/>- resourceLocks物理锁<br/>- locks状态<br/>- queues队列<br/>- subscribers订阅者]
    end

    Writer -->|使用| RefCountMgr
    Writer -->|使用| LockClient
    RefCountMgr -->|使用| RefCountStorage
    LockClient -->|HTTP POST /lock| Handler
    LockClient -->|HTTP POST /unlock| Handler
    LockClient -->|HTTP GET /subscribe SSE| Handler
    Handler -->|调用| LockManager
    LockManager -->|管理| Shard
```

## 核心组件详细设计

### 1. LockManager 结构

```mermaid
classDiagram
    class LockManager {
        -shards: [32]*resourceShard
        -AllowMultiNodeDownload: bool
        +TryLock(request) bool, bool, string
        +Unlock(request) bool
        +getShard(resourceID) *resourceShard
    }

    class resourceShard {
        -mu: sync.RWMutex
        -resourceLocks: map[string]*sync.Mutex
        -locks: map[string]*LockInfo
        -queues: map[string][]*LockRequest
        -subscribers: map[string][]Subscriber
    }

    class LockInfo {
        +Request: *LockRequest
        +AcquiredAt: time.Time
        +Completed: bool
        +Success: bool
        +CompletedAt: time.Time
    }

    class LockRequest {
        +Type: string
        +ResourceID: string
        +NodeID: string
        +Timestamp: time.Time
    }

    LockManager "1" --> "32" resourceShard
    resourceShard "1" --> "*" LockInfo
    resourceShard "1" --> "*" LockRequest
```

### 2. Client 结构

```mermaid
classDiagram
    class LockClient {
        -ServerURL: string
        -ShortClient: *http.Client
        -LongClient: *http.Client
        -NodeID: string
        -MaxRetries: int
        -RetryInterval: time.Duration
        +Lock(ctx, request) *LockResult, error
        +Unlock(ctx, request) error
        -tryLockOnce(ctx, request) *LockResult, error
        -waitForLock(ctx, request) *LockResult, error
    }

    class LockResult {
        +Acquired: bool
        +Error: error
    }

    LockClient --> LockResult
```

### 3. Callback 包结构

```mermaid
classDiagram
    class RefCountManager {
        -storage: RefCountStorage
        +ShouldSkipOperation(type, resourceID) bool, string
        +CanPerformOperation(type, resourceID) bool, string
        +UpdateRefCount(type, resourceID, result)
        +GetRefCount(resourceID) *ReferenceCount
    }

    class RefCountStorage {
        <<interface>>
        +GetRefCount(resourceID) *ReferenceCount
        +SetRefCount(resourceID, refCount)
        +DeleteRefCount(resourceID)
    }

    class ReferenceCount {
        +Count: int
        +Nodes: map[string]bool
    }

    class LocalRefCountStorage {
        -mu: sync.RWMutex
        -refCounts: map[string]*ReferenceCount
        +GetRefCount(resourceID) *ReferenceCount
        +SetRefCount(resourceID, refCount)
        +DeleteRefCount(resourceID)
    }

    RefCountManager --> RefCountStorage
    RefCountStorage <|.. LocalRefCountStorage
    RefCountStorage --> ReferenceCount
```

## 关键流程时序图

### 1. Pull 操作完整流程

```mermaid
sequenceDiagram
    participant Writer as Content Writer
    participant RefCountMgr as RefCountManager
    participant Client as LockClient
    participant Server as LockManager
    participant Shard as resourceShard

    Writer->>RefCountMgr: ShouldSkipOperation(pull, resourceID)
    RefCountMgr->>RefCountMgr: GetRefCount(resourceID)
    alt refCount.Count > 0
        RefCountMgr-->>Writer: skip=true (资源已存在)
        Writer-->>Writer: 跳过操作
    else refCount.Count == 0
        RefCountMgr-->>Writer: skip=false
        Writer->>Client: Lock(request)
        Client->>Server: POST /lock
        Server->>Shard: getShard(resourceID)
        Server->>Shard: 获取分段锁
        Server->>Shard: 检查/创建资源锁
        Server->>Shard: 获取资源锁(物理锁)
        Server->>Shard: 检查locks map
        alt 锁不存在
            Server->>Shard: 创建LockInfo
            Server-->>Client: acquired=true
        else 锁被占用
            alt AllowMultiNodeDownload=false
                Server-->>Client: acquired=false, error="锁被占用"
            else AllowMultiNodeDownload=true
                Server->>Shard: 加入等待队列
                Server-->>Client: acquired=false
                Client->>Client: waitForLock (SSE订阅)
            end
        end
        Client-->>Writer: LockResult
        Writer->>Writer: 执行Pull操作
        Writer->>Client: Unlock(request)
        Client->>Server: POST /unlock
        Server->>Shard: 释放资源锁
        Server->>Shard: 更新LockInfo
        alt 操作成功
            Server->>Shard: 删除锁和资源锁
            Server->>Shard: 广播SSE事件
        else 操作失败
            Server->>Shard: 保留资源锁
            Server->>Shard: processQueue分配锁给下一个
            Server->>Shard: 通知队头节点(SSE)
        end
        Writer->>RefCountMgr: UpdateRefCount(pull, resourceID, result)
        RefCountMgr->>RefCountMgr: refCount.Count++
    end
```

### 2. 分段锁并发处理流程

```mermaid
sequenceDiagram
    participant NodeA as 节点A<br/>resource1
    participant NodeB as 节点B<br/>resource2
    participant NodeC as 节点C<br/>resource1
    participant Shard1 as 分段1
    participant Shard2 as 分段2

    par 不同分段并发
        NodeA->>Shard1: TryLock(resource1)
        Shard1->>Shard1: 获取分段锁
        Shard1->>Shard1: 获取资源锁
        Shard1-->>NodeA: acquired=true
    and
        NodeB->>Shard2: TryLock(resource2)
        Shard2->>Shard2: 获取分段锁
        Shard2->>Shard2: 获取资源锁
        Shard2-->>NodeB: acquired=true
    end

    Note over NodeA,NodeB: 不同分段可以并发执行

    NodeC->>Shard1: TryLock(resource1)
    Shard1->>Shard1: 获取分段锁
    Shard1->>Shard1: 检查locks map
    Shard1-->>NodeC: acquired=false (锁被占用)
    Shard1->>Shard1: 加入等待队列
```

### 3. 物理锁获取顺序

```mermaid
sequenceDiagram
    participant Client
    participant LockManager
    participant Shard as resourceShard

    Client->>LockManager: TryLock(request)
    LockManager->>Shard: getShard(resourceID)
    
    Note over Shard: 阶段1: 获取分段锁，检查/创建资源锁
    LockManager->>Shard: shard.mu.Lock()
    alt 资源锁存在
        LockManager->>Shard: 获取resourceLock引用
    else 资源锁不存在
        LockManager->>Shard: 创建resourceLock
        LockManager->>Shard: 加入resourceLocks map
    end
    LockManager->>Shard: shard.mu.Unlock()
    
    Note over Shard: 阶段2: 获取资源锁(物理锁)
    LockManager->>Shard: resourceLock.Lock()
    
    Note over Shard: 阶段3: 重新获取分段锁访问locks map
    LockManager->>Shard: shard.mu.RLock()
    LockManager->>Shard: 检查locks[key]
    LockManager->>Shard: shard.mu.RUnlock()
    
    Note over Shard: 阶段4: 根据检查结果处理
    alt 锁不存在
        LockManager->>Shard: shard.mu.Lock()
        LockManager->>Shard: 创建LockInfo
        LockManager->>Shard: shard.mu.Unlock()
    end
    
    LockManager->>Shard: resourceLock.Unlock()
    LockManager-->>Client: 返回结果
```

## 数据流图

### Pull 操作数据流

```mermaid
flowchart TD
    Start([节点请求Pull资源]) --> CheckRefCount{检查引用计数<br/>ShouldSkipOperation}
    CheckRefCount -->|refCount > 0| Skip[跳过操作<br/>资源已存在]
    CheckRefCount -->|refCount == 0| TryLock[请求锁<br/>POST /lock]
    
    TryLock --> GetShard[getShard<br/>哈希resourceID]
    GetShard --> ShardLock[获取分段锁]
    ShardLock --> CheckResourceLock{资源锁是否存在?}
    
    CheckResourceLock -->|不存在| CreateResourceLock[创建资源锁<br/>加入resourceLocks]
    CheckResourceLock -->|存在| GetResourceLock[获取资源锁引用]
    
    CreateResourceLock --> ReleaseShardLock1[释放分段锁]
    GetResourceLock --> ReleaseShardLock1
    ReleaseShardLock1 --> AcquireResourceLock[获取资源锁<br/>物理锁]
    
    AcquireResourceLock --> ReacquireShardLock[重新获取分段锁<br/>读锁]
    ReacquireShardLock --> CheckLockInfo{检查locks map}
    
    CheckLockInfo -->|锁不存在| CreateLockInfo[创建LockInfo<br/>获取锁成功]
    CheckLockInfo -->|锁被占用| CheckMultiNode{多节点下载<br/>模式开启?}
    
    CheckMultiNode -->|关闭| ReturnFail[返回失败<br/>不加入队列]
    CheckMultiNode -->|开启| AddToQueue[加入等待队列]
    AddToQueue --> WaitLock[SSE订阅等待]
    
    CreateLockInfo --> ExecuteOp[执行Pull操作]
    WaitLock -->|锁被分配| ExecuteOp
    
    ExecuteOp --> UpdateRefCount[更新引用计数<br/>refCount++]
    UpdateRefCount --> Unlock[释放锁<br/>POST /unlock]
    
    Unlock --> CheckSuccess{操作成功?}
    CheckSuccess -->|成功| DeleteLock[删除锁和资源锁<br/>广播SSE事件]
    CheckSuccess -->|失败| KeepResourceLock[保留资源锁<br/>分配锁给队列下一个]
    
    DeleteLock --> End([完成])
    KeepResourceLock --> End
    Skip --> End
    ReturnFail --> End
```

## 分段锁设计

### 分段锁结构

```mermaid
graph TB
    subgraph LockManager["LockManager"]
        Shards[32个resourceShard]
    end

    subgraph Shard["resourceShard (单个分段)"]
        ShardLock[分段锁<br/>sync.RWMutex]
        ResourceLocks[resourceLocks<br/>map[string]*sync.Mutex<br/>物理锁]
        Locks[locks<br/>map[string]*LockInfo<br/>锁状态]
        Queues[queues<br/>map[string][]*LockRequest<br/>等待队列]
        Subscribers[subscribers<br/>map[string][]Subscriber<br/>SSE订阅者]
    end

    Shards --> Shard
    Shard --> ShardLock
    Shard --> ResourceLocks
    Shard --> Locks
    Shard --> Queues
    Shard --> Subscribers
```

### 分段锁哈希分布

```mermaid
graph LR
    Resource1[resource1] -->|FNV-1a Hash| Shard1[分段1]
    Resource2[resource2] -->|FNV-1a Hash| Shard2[分段2]
    Resource3[resource3] -->|FNV-1a Hash| Shard3[分段3]
    ResourceN[resourceN] -->|FNV-1a Hash| Shard32[分段32]
    
    Shard1 --> Concurrent1[并发处理]
    Shard2 --> Concurrent2[并发处理]
    Shard3 --> Concurrent3[并发处理]
    Shard32 --> Concurrent32[并发处理]
    
    Note1[同一resourceID的所有操作类型<br/>分到同一个分段]
```

## 配置选项

### 多节点下载模式

```mermaid
stateDiagram-v2
    [*] --> 检查配置: TryLock请求
    检查配置 --> 多节点模式开启: AllowMultiNodeDownload=true
    检查配置 --> 多节点模式关闭: AllowMultiNodeDownload=false
    
    多节点模式开启 --> 锁被占用: 检查锁状态
    锁被占用 --> 加入队列: 加入等待队列
    加入队列 --> SSE订阅: 等待锁释放
    
    多节点模式关闭 --> 锁被占用: 检查锁状态
    锁被占用 --> 直接返回失败: 不加入队列
    
    多节点模式开启 --> 锁可用: 检查锁状态
    多节点模式关闭 --> 锁可用: 检查锁状态
    锁可用 --> 获取锁成功: 创建LockInfo
```

## 引用计数管理

### 引用计数更新规则

```mermaid
stateDiagram-v2
    [*] --> Pull操作: 操作成功
    [*] --> Update操作: 操作成功
    [*] --> Delete操作: 操作成功
    
    Pull操作 --> 引用计数加1: refCount.Count++
    Update操作 --> 引用计数不变: 不改变
    Delete操作 --> 删除引用计数: DeleteRefCount
    
    Pull操作 --> 操作失败: 操作失败
    Update操作 --> 操作失败: 操作失败
    Delete操作 --> 操作失败: 操作失败
    操作失败 --> 引用计数不变: 不更新
```

## 组件交互图

### 完整系统交互

```mermaid
graph TB
    subgraph ContentLayer["Content 插件层"]
        Writer[Writer]
        RefCountMgr[RefCountManager]
        RefCountStorage[RefCountStorage<br/>本地存储]
    end

    subgraph ClientLayer["Client 层"]
        LockClient[LockClient<br/>ShortClient/LongClient]
    end

    subgraph ServerLayer["Server 层"]
        Handler[Handler<br/>HTTP接口]
        LockManager[LockManager<br/>32分段锁]
        Shard[resourceShard<br/>物理锁+状态+队列]
    end

    Writer -->|1. ShouldSkipOperation| RefCountMgr
    RefCountMgr -->|2. GetRefCount| RefCountStorage
    Writer -->|3. Lock| LockClient
    LockClient -->|4. POST /lock| Handler
    Handler -->|5. TryLock| LockManager
    LockManager -->|6. 管理| Shard
    Writer -->|7. 执行操作| Writer
    Writer -->|8. UpdateRefCount| RefCountMgr
    Writer -->|9. Unlock| LockClient
    LockClient -->|10. POST /unlock| Handler
    Handler -->|11. Unlock| LockManager
```

## 关键设计决策

### 1. 物理锁设计

- **分段锁**：`sync.RWMutex`，保护资源锁的创建和访问
- **资源锁**：`sync.Mutex`，每个资源一个真实的互斥锁
- **锁状态**：`LockInfo`，存储锁的元数据

### 2. 分段锁优势

- **并发度提升**：32个分段，不同分段可以并发处理
- **哈希分布**：使用 FNV-1a 算法，确保同一 resourceID 的所有操作类型分到同一分段

### 3. 多节点下载模式

- **开启**：锁被占用时加入等待队列，支持多节点协作
- **关闭**：锁被占用时直接返回失败，只允许单节点操作

### 4. 引用计数管理

- **客户端判断**：Content 插件在获取锁之前判断是否应该执行操作
- **本地存储**：使用 `RefCountStorage` 接口，支持内存/文件/数据库等实现

## 性能特性

### 并发性能

- **分段锁**：32个分段，理论上可以支持32个不同资源的并发操作
- **物理锁**：每个资源独立的互斥锁，减少锁竞争
- **读写分离**：分段锁使用 `RWMutex`，读操作可以并发

### 扩展性

- **分段数量**：可以根据并发需求调整 `shardCount`
- **存储实现**：`RefCountStorage` 接口支持不同的存储后端
- **配置灵活**：通过环境变量配置多节点下载模式

## 相关文档

- [物理锁实现方案](./PHYSICAL_LOCK_FINAL_PLAN.md)
- [分段锁性能分析](./SHARD_LOCK_PERFORMANCE_SUMMARY.md)
- [引用计数存储设计](./REFCOUNT_STORAGE_DESIGN.md)
- [客户端使用指南](./CLIENT_DEBUG_QUICK_START.md)
