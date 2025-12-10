# 分布式锁系统流程图

## 1. 加锁流程（TryLock）

```mermaid
flowchart TD
    A[客户端发送加锁请求] --> B{解析请求参数}
    B -->|参数无效| C[返回400错误]
    B -->|参数有效| D[计算资源Key<br/>Type:ResourceID]
    D --> E[根据Key哈希获取分段Shard]
    E --> F[获取分段锁 shard.mu.Lock]
    F --> G{操作类型判断}
    
    G -->|Delete操作| H{检查引用计数}
    H -->|引用计数 > 0| I[返回错误<br/>无法删除: 有节点正在使用]
    H -->|引用计数 = 0| J{检查是否已有锁}
    
    G -->|Update操作| K{配置检查<br/>UpdateRequiresNoRef?}
    K -->|true 且引用计数 > 0| L[返回错误<br/>无法更新: 有节点正在使用]
    K -->|false 或引用计数 = 0| J
    
    G -->|Pull操作| J
    
    J -->|锁已存在| M{锁状态检查}
    M -->|已完成且成功| N[返回 skip=true<br/>跳过操作]
    M -->|已完成但失败| O[删除锁<br/>处理队列下一个请求]
    O --> P{队列是否为空}
    P -->|不为空| Q[分配锁给队列第一个请求]
    Q --> R[返回 acquired=true]
    P -->|为空| S[返回 acquired=false]
    M -->|未完成| T[加入等待队列FIFO]
    T --> S
    
    J -->|锁不存在| U[创建锁信息<br/>LockInfo]
    U --> R
    
    R --> V[释放分段锁 shard.mu.Unlock]
    S --> V
    I --> V
    L --> V
    N --> V
    V --> W[返回响应给客户端]
```

## 2. 解锁流程（Unlock）

```mermaid
flowchart TD
    A["客户端发送解锁请求"]
    --> B{"解析请求参数"}
    
    B -->|"参数无效"| C["返回400错误"]
    B -->|"参数有效"| D["计算资源Key\nType: ResourceID"]
    
    D --> E["根据Key哈希获取分段Shard"]
    E --> F["获取分段锁 shard.mu.Lock"]
    F --> G{"检查锁是否存在"}
    
    G -->|"锁不存在"| H["返回403错误\n锁不存在"]
    G -->|"锁存在"| I{"检查是否为锁持有者"}
    
    I -->|"不是持有者"| J["返回403错误\n不是锁的持有者"]
    I -->|"是持有者"| K["更新锁信息\nCompleted=true\nSuccess=request.Success"]
    
    K --> L{"操作是否成功?"}
    
    L -->|"成功"| M{"操作类型判断"}
    M -->|"Pull"| N["更新引用计数\nCount++\nNodes[nodeID]=true"]
    M -->|"Update"| O["不改变引用计数"]
    M -->|"Delete"| P["清理引用计数\ndelete refCounts"]
    
    L -->|"失败"| Q["立即释放锁\ndelete locks[key]"]
    Q --> R["处理队列下一个请求"]
    R --> S{"队列是否为空"}
    S -->|"不为空"| T["分配锁给队列第一个请求\nFIFO"]
    S -->|"为空"| U["完成"]
    
    N --> V["保留锁信息5分钟\n用于其他节点查询状态"]
    O --> V
    P --> V
    V --> W["启动goroutine\n5分钟后清理锁信息"]
    W --> U
    
    T --> U
    H --> X["释放分段锁 shard.mu.Unlock"]
    J --> X
    U --> X
    X --> Y["返回响应给客户端"]

```

## 3. 引用计数管理流程

```mermaid
flowchart TD
    A[引用计数操作] --> B{操作类型}
    
    B -->|Pull成功| C[获取或创建ReferenceCount]
    C --> D{节点是否已在集合中?}
    D -->|否| E[Count++<br/>Nodes[nodeID] = true]
    D -->|是| F[不改变计数<br/>防止重复计数]
    
    B -->|Update| G[不改变引用计数]
    
    B -->|Delete成功| H[删除引用计数条目<br/>delete refCounts[resourceID]]
    
    E --> I[更新完成]
    F --> I
    G --> I
    H --> I
```

## 4. 客户端重试机制流程

```mermaid
flowchart TD
    A[客户端发起请求] --> B[设置请求超时<br/>RequestTimeout]
    B --> C[序列化请求数据]
    C --> D[创建HTTP请求<br/>带Context超时]
    D --> E[发送HTTP请求]
    
    E --> F{请求结果}
    F -->|成功| G[解析响应]
    F -->|超时| H{是否达到最大重试次数?}
    F -->|连接失败| H
    F -->|网络错误| H
    
    H -->|未达到| I[等待RetryInterval]
    I --> J[重试计数+1]
    J --> C
    
    H -->|已达到| K[返回错误<br/>已重试N次]
    
    G --> L{响应状态}
    L -->|有错误信息| M[返回LockResult<br/>Error != nil]
    L -->|skip=true| N[返回LockResult<br/>Skipped=true]
    L -->|acquired=true| O[返回LockResult<br/>Acquired=true]
    L -->|acquired=false| P[进入等待轮询]
    
    P --> Q[每500ms查询锁状态]
    Q --> R{状态检查}
    R -->|已完成且成功| S[返回Skipped=true]
    R -->|已完成但失败| T[重新尝试获取锁]
    R -->|获得锁| O
    R -->|继续等待| Q
    
    T --> E
```

## 5. 分段锁并发处理流程

```mermaid
flowchart TD
    A[多个并发请求] --> B[计算资源Key]
    B --> C[FNV-1a哈希算法]
    C --> D[取模获取分段索引<br/>hash % shardCount]
    
    D --> E{分段分布}
    E -->|不同分段| F[并发执行<br/>不同分段独立锁]
    E -->|相同分段| G[串行执行<br/>同一分段共享锁]
    
    F --> H[分段1: 处理请求1]
    F --> I[分段2: 处理请求2]
    F --> J[分段N: 处理请求N]
    
    G --> K[请求1获取锁]
    K --> L[请求2进入队列]
    L --> M[请求3进入队列]
    M --> N[请求1完成]
    N --> O[请求2获得锁<br/>FIFO]
    
    H --> P[完成]
    I --> P
    J --> P
    O --> P
```

## 6. 完整操作流程（Pull示例）

```mermaid
sequenceDiagram
    participant C as 客户端
    participant LM as 锁管理器
    participant S as 分段Shard
    participant RC as 引用计数
    
    C->>LM: POST /lock (Type=pull)
    LM->>S: 计算分段并获取锁
    S->>S: shard.mu.Lock()
    S->>S: 检查是否已有锁
    alt 锁不存在
        S->>S: 创建LockInfo
        S-->>LM: acquired=true
    else 锁已存在且完成
        S-->>LM: skip=true
    else 锁已存在且未完成
        S->>S: 加入FIFO队列
        S-->>LM: acquired=false
    end
    S->>S: shard.mu.Unlock()
    LM-->>C: 返回响应
    
    alt 获得锁
        C->>C: 执行pull操作
        C->>LM: POST /unlock (Success=true)
        LM->>S: 获取分段锁
        S->>S: 验证锁持有者
        S->>RC: 更新引用计数
        RC->>RC: Count++<br/>Nodes[nodeID]=true
        S->>S: 标记锁为已完成
        S->>S: shard.mu.Unlock()
        LM-->>C: 解锁成功
    end
```

## 7. Delete操作特殊检查流程

```mermaid
flowchart TD
    A[Delete操作请求] --> B[获取分段锁]
    B --> C[获取引用计数]
    C --> D{引用计数检查}
    
    D -->|Count > 0| E[返回错误<br/>无法删除: 有节点正在使用]
    D -->|Count = 0| F{检查是否已有锁}
    
    F -->|锁已存在| G{锁状态}
    G -->|已完成| H[处理队列或跳过]
    G -->|未完成| I[加入等待队列]
    
    F -->|锁不存在| J[创建锁信息]
    J --> K[返回acquired=true]
    
    K --> L[执行delete操作]
    L --> M{操作结果}
    M -->|成功| N[清理引用计数<br/>delete refCounts]
    M -->|失败| O[释放锁<br/>处理队列]
    
    N --> P[标记锁为已完成]
    O --> P
    P --> Q[完成]
    
    E --> Q
    H --> Q
    I --> Q
```

## 关键点说明

1. **分段锁机制**：通过哈希将资源分配到不同分段，提升并发度
2. **FIFO队列**：确保请求按顺序获得锁
3. **引用计数检查**：delete操作必须引用计数为0，update操作可配置
4. **重试机制**：客户端自动重试网络错误和超时
5. **原子性保证**：所有检查和更新都在分段锁保护下进行
6. **状态管理**：锁的状态（未完成/已完成/成功/失败）用于决定后续请求的处理

