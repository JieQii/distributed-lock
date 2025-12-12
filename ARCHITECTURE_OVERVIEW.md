# 分布式锁系统架构概览

> 现设计：服务器只管锁与排队；引用计数与“做/不做”在 Content 插件本地（`callback` 包 + 自定义存储）。下述流程/时序均适配 GitHub（Mermaid）。

## 系统架构图

```
┌───────────────────────────────────────────────────────────────┐
│                         Content 插件                          │
│  ┌─────────────────────────────────────────────────────────┐  │
│  │ Writer / 业务逻辑                                       │  │
│  │ - callback.ShouldSkipOperation / UpdateRefCount         │  │
│  │ - client.Lock / client.Unlock                           │  │
│  └─────────────────────────────────────────────────────────┘  │
└───────────────────────────────────────────────────────────────┘
                           │ HTTP
                           ▼
┌───────────────────────────────────────────────────────────────┐
│                           Client                              │
│  - 重试 / 超时 / 轮询                                         │
│  - POST /lock, POST /unlock                                   │
└───────────────────────────────────────────────────────────────┘
                           │ HTTP
                           ▼
┌───────────────────────────────────────────────────────────────┐
│                           Server                              │
│  Handler → LockManager                                        │
│  LockManager:                                                  │
│    - TryLock: 互斥 + 队列（单 FIFO/资源键）                    │
│    - Unlock: 校验 holder, 释放并推进队列                      │
└───────────────────────────────────────────────────────────────┘
```

## 类图（简）

```
Content/Writer ──uses──> Client/LockClient ──HTTP──> Server/Handler ──calls──> Server/LockManager
Content/Writer ──uses──> callback.RefCountManager + 自定义 RefCountStorage(本地文件/DB)
```

## 类图

### 核心类关系

```
┌─────────────────────────────────────────────────────────────┐
│                    Content 插件层                            │
└─────────────────────────────────────────────────────────────┘
                          │
                          │ 使用
                          ▼
┌─────────────────────────────────────────────────────────────┐
│  Writer                                                      │
│  + client: *LockClient                                      │
│  + resourceID: string                                       │
│  + locked: bool                                             │
│  + skipped: bool                                            │
│  + OpenWriter() → *Writer                                   │
│  + Write() → (int, error)                                   │
│  + Commit() → error                                         │
│  + Close() → error                                          │
└─────────────────────────────────────────────────────────────┘
                          │
                          │ 使用
                          ▼
┌─────────────────────────────────────────────────────────────┐
│  LockClient (client包)                                        │
│  + ServerURL: string                                        │
│  + NodeID: string                                           │
│  + MaxRetries: int                                          │
│  + RetryInterval: time.Duration                             │
│  + RequestTimeout: time.Duration                            │
│  + Lock() → (*LockResult, error)                            │
│  + Unlock() → error                                         │
│  + tryLockOnce() → (*LockResult, error)                     │
│  + waitForLock() → (*LockResult, error)                     │
└─────────────────────────────────────────────────────────────┘
                          │
                          │ HTTP请求
                          ▼
┌─────────────────────────────────────────────────────────────┐
│  Handler (server包)                                           │
│  + lockManager: *LockManager                                │
│  + Lock() → HTTP处理                                        │
│  + Unlock() → HTTP处理                                      │
│  + LockStatus() → HTTP处理                                   │
│  + GetRefCount() → HTTP处理                                 │
└─────────────────────────────────────────────────────────────┘
                          │
                          │ 调用
                          ▼
┌─────────────────────────────────────────────────────────────┐
│  LockManager (server包)                                       │
│  + shards: [32]*resourceShard                              │
│  + UpdateRequiresNoRef: bool                                │
│  + refCountManager: *callback.RefCountManager              │
│  + TryLock() → (bool, bool, string)                        │
│  + Unlock() → bool                                          │
│  + GetLockStatus() → (bool, bool, bool)                      │
│  + GetRefCount() → *callback.ReferenceCount                │
└─────────────────────────────────────────────────────────────┘
                          │
                          │ 使用
                          ▼
┌─────────────────────────────────────────────────────────────┐
│  RefCountManager (callback包)                                │
│  + storage: RefCountStorage                                 │
│  + ShouldSkipOperation() → (bool, string)                   │
│  + CanPerformOperation() → (bool, string)                   │
│  + UpdateRefCount()                                         │
│  + GetRefCount() → *ReferenceCount                          │
└─────────────────────────────────────────────────────────────┘
                          │
                          │ 使用
                          ▼
┌─────────────────────────────────────────────────────────────┐
│  RefCountStorage (接口)                                       │
│  + GetRefCount() → *ReferenceCount                          │
│  + SetRefCount()                                            │
│  + DeleteRefCount()                                         │
└─────────────────────────────────────────────────────────────┘
                          ▲
                          │ 实现
                          │
        ┌─────────────────┴─────────────────┐
        │                                     │
┌───────┴────────┐                  ┌────────┴────────┐
│ refCountStorage│                  │ 自定义存储       │
│ (server端适配器)│                  │ (content插件)   │
└────────────────┘                  └─────────────────┘
```

## 调用链分析

### 场景1：Content插件通过Server端获取锁（当前实现）

```
Content插件
  │
  ├─> Writer.OpenWriter()
  │     │
  │     └─> client.ClusterLock()
  │           │
  │           └─> LockClient.Lock()
  │                 │
  │                 └─> LockClient.tryLockOnce()
  │                       │
  │                       └─> HTTP POST /lock
  │                             │
  │                             ▼
Server端
  │
  ├─> Handler.Lock()
  │     │
  │     └─> LockManager.TryLock()
  │           │
  │           ├─> 检查锁状态
  │           │     └─> shard.locks[key]
  │           │
  │           └─> 检查引用计数
  │                 │
  │                 └─> refCountManager.ShouldSkipOperation()
  │                       │
  │                       └─> refCountManager.CanPerformOperation()
  │                             │
  │                             └─> storage.GetRefCount()
  │                                   │
  │                                   └─> refCountStorage.GetRefCount()
  │                                         │
  │                                         └─> shard.refCounts[resourceID]
  │
  └─> 返回响应 {acquired, skip, error}
        │
        ▼
Client端
  │
  └─> 解析响应
        │
        └─> 返回 LockResult {Acquired, Skipped, Error}
              │
              ▼
Content插件
  │
  └─> 根据 result.Skipped 决定是否执行操作
```

### 场景2：Content插件直接使用Callback包（推荐方式）

```
Content插件
  │
  ├─> 创建自定义存储
  │     └─> MyStorage (实现 RefCountStorage)
  │
  ├─> 创建管理器
  │     └─> callback.NewRefCountManager(storage)
  │
  ├─> 判断是否应该执行操作
  │     └─> manager.ShouldSkipOperation()
  │           │
  │           └─> storage.GetRefCount()
  │                 │
  │                 └─> 从本地存储（文件/数据库）读取
  │
  ├─> 如果需要，获取分布式锁
  │     └─> client.Lock() → Server端
  │
  ├─> 执行操作
  │     └─> downloadLayer() / updateLayer() / deleteLayer()
  │
  └─> 更新引用计数
        └─> manager.UpdateRefCount()
              │
              └─> storage.SetRefCount()
                    │
                    └─> 保存到本地存储（文件/数据库）
```

## 关于计数文件的设计讨论

### 当前实现的问题

**当前设计**：Server端在 `TryLock` 中根据引用计数判断是否给锁或跳过操作

```go
// server/lock_manager.go
skip, errMsg := lm.refCountManager.ShouldSkipOperation(request.Type, request.ResourceID)
if skip {
    return false, true, ""  // 返回 skip=true，告诉客户端跳过操作
}
```

**问题**：
1. **职责混淆**：分布式锁应该只负责锁的获取和释放，不应该判断业务操作是否应该执行
2. **耦合业务逻辑**：Server端需要了解"什么时候应该跳过操作"的业务语义
3. **扩展性差**：如果不同场景有不同的判断规则，Server端需要支持更多配置

### 推荐的设计方案

**分布式锁的职责**：
- ✅ 保证同一资源同一时间只有一个节点在操作（互斥）
- ✅ 管理等待队列（FIFO）
- ✅ 提供锁的获取和释放

**引用计数/计数文件的职责**：
- ✅ 判断操作是否应该执行（业务逻辑）
- ✅ 管理资源的引用计数
- ✅ 应该在 **Content插件** 中调用 callback 包来判断

### 推荐的重构方案

#### 方案1：完全分离（推荐）

**Server端**：只负责锁管理，不判断是否跳过操作

```go
// server/lock_manager.go
func (lm *LockManager) TryLock(request *LockRequest) (bool, string) {
    // 只检查锁状态，不检查引用计数
    // 返回：是否获得锁，错误信息
    // 不再返回 skip 参数
}
```

**Content插件**：在获取锁之前，先调用 callback 包判断

```go
// content/writer.go
func OpenWriter(ctx context.Context, serverURL, nodeID, resourceID string) (*Writer, error) {
    // 1. 先判断是否应该执行操作（使用callback包）
    manager := callback.NewRefCountManager(storage)
    skip, _ := manager.ShouldSkipOperation(callback.OperationTypePull, resourceID)
    if skip {
        // 跳过操作，不需要获取锁
        return &Writer{skipped: true}, nil
    }
    
    // 2. 如果需要执行操作，再获取分布式锁
    result, err := client.Lock(ctx, request)
    if err != nil {
        return nil, err
    }
    
    // 3. 获得锁后执行操作
    // ...
}
```

#### 方案2：保留当前设计但明确职责

如果保留当前设计，需要明确：
- Server端的引用计数判断是为了**优化**（避免不必要的锁获取）
- Content插件仍然应该**独立判断**，不依赖Server端的判断

## 数据流图

### Pull操作流程

```
节点A请求Pull资源R
  │
  ├─> [Content插件] 调用 callback.ShouldSkipOperation()
  │     │
  │     └─> 检查本地计数文件
  │           │
  │           ├─> refcount > 0 → 跳过操作（不获取锁）
  │           └─> refcount == 0 → 继续
  │
  ├─> [Client] POST /lock
  │     │
  │     └─> [Server] TryLock()
  │           │
  │           ├─> 检查锁状态
  │           │     ├─> 有锁且未完成 → 加入队列
  │           │     └─> 无锁 → 继续
  │           │
  │           └─> 获取锁成功 → 返回 acquired=true
  │
  ├─> [Content插件] 执行Pull操作
  │     └─> downloadLayer()
  │
  └─> [Content插件] Commit()
        │
        ├─> [Client] POST /unlock (Success=true)
        │     │
        │     └─> [Server] Unlock()
        │           │
        │           ├─> 释放锁
        │           └─> 更新引用计数
        │                 │
        │                 └─> callback.UpdateRefCount()
        │                       │
        │                       └─> 更新计数文件
        │
        └─> [Content插件] 更新本地计数文件
              └─> callback.UpdateRefCount()
```

## 关键设计决策

### 1. 引用计数判断的位置

**当前**：Server端在 `TryLock` 中判断
**推荐**：Content插件在获取锁之前判断

### 2. 计数文件的存储位置

**选项A**：Server端统一管理（当前）
- 优点：集中管理，数据一致
- 缺点：Server端需要持久化，增加复杂度

**选项B**：Content插件本地管理（推荐）
- 优点：解耦，灵活，可以本地文件/数据库
- 缺点：需要同步机制（如果需要多节点一致性）

**选项C**：混合方案
- Server端：管理分布式环境下的引用计数（用于锁判断）
- Content插件：管理本地计数文件（用于业务判断）

## 建议的重构步骤

1. **第一步**：Content插件在获取锁之前调用 callback 包判断
2. **第二步**：Server端移除引用计数判断逻辑，只负责锁管理
3. **第三步**：Content插件实现计数文件的本地存储和管理

这样设计更符合单一职责原则，分布式锁只负责锁管理，业务逻辑在业务层。

