# 分布式锁系统架构文档

## 系统架构概览

```
┌─────────────────────────────────────────────────────────────┐
│                    Content 插件                              │
│  ┌───────────────────────────────────────────────────────┐  │
│  │  Writer (content/writer.go)                           │  │
│  │  - OpenWriter() → 调用 client.Lock()                 │  │
│  │  - 使用 callback 包判断是否执行操作                  │  │
│  └───────────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────┘
                          │
                          │ HTTP请求
                          ▼
┌─────────────────────────────────────────────────────────────┐
│                    Client 包                                │
│  ┌───────────────────────────────────────────────────────┐  │
│  │  LockClient (client/client.go)                       │  │
│  │  - Lock() → POST /lock                               │  │
│  │  - Unlock() → POST /unlock                           │  │
│  │  - 处理重试、超时等                                   │  │
│  └───────────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────┘
                          │
                          │ HTTP
                          ▼
┌─────────────────────────────────────────────────────────────┐
│                    Server 端                                │
│  ┌───────────────────────────────────────────────────────┐  │
│  │  Handler (server/handler.go)                          │  │
│  │  - Lock() → 调用 LockManager.TryLock()               │  │
│  │  - Unlock() → 调用 LockManager.Unlock()              │  │
│  └───────────────────────────────────────────────────────┘  │
│  ┌───────────────────────────────────────────────────────┐  │
│  │  LockManager (server/lock_manager.go)                │  │
│  │  - TryLock() → 锁管理 + 引用计数判断                  │  │
│  │  - Unlock() → 释放锁 + 更新引用计数                   │  │
│  │  - 使用 callback.RefCountManager                      │  │
│  └───────────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────┘
                          │
                          │ 使用
                          ▼
┌─────────────────────────────────────────────────────────────┐
│                    Callback 包                              │
│  ┌───────────────────────────────────────────────────────┐  │
│  │  RefCountManager (callback/manager.go)                │  │
│  │  - ShouldSkipOperation()                             │  │
│  │  - CanPerformOperation()                             │  │
│  │  - UpdateRefCount()                                   │  │
│  │  - GetRefCount()                                      │  │
│  └───────────────────────────────────────────────────────┘  │
│  ┌───────────────────────────────────────────────────────┐  │
│  │  RefCountStorage (callback/types.go)                  │  │
│  │  - GetRefCount()                                      │  │
│  │  - SetRefCount()                                      │  │
│  │  - DeleteRefCount()                                   │  │
│  └───────────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────┘
```

## 类图

### 核心类关系

```
┌─────────────────────────────────────────────────────────────┐
│                      Content 插件                          │
│                                                             │
│  ┌──────────────────┐         ┌──────────────────┐        │
│  │     Writer       │────────▶│  LockClient       │        │
│  │                  │  uses   │  (client包)       │        │
│  │  - OpenWriter()  │         │                   │        │
│  │  - Write()       │         │  - Lock()         │        │
│  │  - Commit()     │         │  - Unlock()      │        │
│  │  - Close()      │         └──────────────────┘        │
│  └──────────────────┘                                      │
│         │                                                  │
│         │ uses                                             │
│         ▼                                                  │
│  ┌──────────────────┐                                      │
│  │ RefCountManager  │                                      │
│  │ (callback包)      │                                      │
│  │                  │                                      │
│  │ - ShouldSkip...  │                                      │
│  │ - UpdateRef...   │                                      │
│  └──────────────────┘                                      │
└─────────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────────┐
│                      Server 端                              │
│                                                             │
│  ┌──────────────────┐         ┌──────────────────┐        │
│  │     Handler      │────────▶│  LockManager      │        │
│  │                  │  uses   │                   │        │
│  │  - Lock()        │         │  - TryLock()      │        │
│  │  - Unlock()      │         │  - Unlock()       │        │
│  │  - GetRefCount() │         │  - GetRefCount()  │        │
│  └──────────────────┘         └──────────────────┘        │
│                                      │                      │
│                                      │ uses                │
│                                      ▼                      │
│                            ┌──────────────────┐            │
│                            │ RefCountManager   │            │
│                            │ (callback包)      │            │
│                            │                   │            │
│                            │ - ShouldSkip...   │            │
│                            │ - UpdateRef...    │            │
│                            └──────────────────┘            │
│                                      │                      │
│                                      │ uses                │
│                                      ▼                      │
│                            ┌──────────────────┐            │
│                            │ refCountStorage   │            │
│                            │ (适配器)          │            │
│                            │                   │            │
│                            │ 实现RefCount...   │            │
│                            └──────────────────┘            │
└─────────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────────┐
│                    Callback 包                              │
│                                                             │
│  ┌──────────────────┐         ┌──────────────────┐        │
│  │ RefCountManager  │────────▶│ RefCountStorage   │        │
│  │                  │  uses   │ (接口)            │        │
│  │                  │         │                   │        │
│  │ - UpdateRef...    │         │ - GetRefCount()   │        │
│  │ - ShouldSkip...   │         │ - SetRefCount()   │        │
│  │ - CanPerform...   │         │ - DeleteRef...    │        │
│  └──────────────────┘         └──────────────────┘        │
│                                      ▲                      │
│                                      │                      │
│                    ┌─────────────────┴──────────┐          │
│                    │                             │          │
│         ┌──────────────────┐      ┌──────────────────┐     │
│         │ refCountStorage   │      │ 自定义存储实现    │     │
│         │ (server端适配器)   │      │ (content插件)     │     │
│         └──────────────────┘      └──────────────────┘     │
└─────────────────────────────────────────────────────────────┘
```

## 调用链

### 1. Content插件获取锁并执行操作

```
Content插件
  │
  ├─> Writer.OpenWriter()
  │     │
  │     ├─> client.LockClient.Lock()
  │     │     │
  │     │     ├─> HTTP POST /lock
  │     │     │     │
  │     │     │     └─> server.Handler.Lock()
  │     │     │           │
  │     │     │           └─> server.LockManager.TryLock()
  │     │     │                 │
  │     │     │                 ├─> 检查锁状态
  │     │     │                 │
  │     │     │                 └─> callback.RefCountManager.ShouldSkipOperation()
  │     │     │                       │
  │     │     │                       └─> callback.RefCountStorage.GetRefCount()
  │     │     │
  │     │     └─> 返回 {acquired, skip, error}
  │     │
  │     └─> 根据结果设置 writer.skipped 或 writer.locked
  │
  ├─> Writer.Write() (如果未跳过)
  │
  └─> Writer.Commit()
        │
        └─> client.LockClient.Unlock()
              │
              ├─> HTTP POST /unlock
              │     │
              │     └─> server.Handler.Unlock()
              │           │
              │           └─> server.LockManager.Unlock()
              │                 │
              │                 └─> callback.RefCountManager.UpdateRefCount()
              │                       │
              │                       └─> callback.RefCountStorage.SetRefCount()
```

### 2. Content插件独立使用callback包（不依赖server端）

```
Content插件
  │
  ├─> 创建自定义存储
  │     └─> 实现 callback.RefCountStorage 接口
  │
  ├─> callback.NewRefCountManager(storage)
  │
  ├─> manager.ShouldSkipOperation()
  │     │
  │     └─> storage.GetRefCount()
  │
  ├─> 执行操作（如果未跳过）
  │
  └─> manager.UpdateRefCount()
        │
        └─> storage.SetRefCount()
```

## 当前设计问题分析

### 问题：引用计数判断在Server端

**当前实现**：
- Server端在 `TryLock()` 中调用 `refCountManager.ShouldSkipOperation()`
- 根据引用计数决定是否返回 `skip = true`
- Content插件被动接收server端的判断结果

**问题**：
1. **职责混淆**：Server端既管理锁，又判断业务逻辑（是否执行操作）
2. **耦合度高**：Server端需要了解业务语义（什么时候应该跳过操作）
3. **不灵活**：不同业务场景可能需要不同的判断逻辑

### 建议设计：引用计数判断在Content插件

**理想设计**：
- **Server端**：只负责锁管理（给不给锁），不判断是否执行操作
- **Content插件**：通过callback包判断是否执行操作
- **职责分离**：锁管理 vs 业务逻辑判断

## 设计调整方案

### 方案：Server端只管理锁，不判断引用计数

**Server端职责**：
- 检查锁是否被占用
- 管理等待队列
- 分配锁给请求者
- **不判断**是否应该跳过操作

**Content插件职责**：
- 获取锁后，通过callback包判断是否执行操作
- 如果引用计数判断应该跳过，直接返回，不执行实际操作
- 如果应该执行，执行操作后更新引用计数

### 调整后的调用链

```
Content插件
  │
  ├─> Writer.OpenWriter()
  │     │
  │     ├─> client.LockClient.Lock()
  │     │     │
  │     │     └─> HTTP POST /lock
  │     │           │
  │     │           └─> server.Handler.Lock()
  │     │                 │
  │     │                 └─> server.LockManager.TryLock()
  │     │                       │
  │     │                       └─> 只检查锁状态，不判断引用计数
  │     │                             │
  │     │                             └─> 返回 {acquired, false, ""}
  │     │
  │     └─> 如果 acquired = true
  │           │
  │           └─> callback.RefCountManager.ShouldSkipOperation()
  │                 │
  │                 └─> 如果 skip = true，设置 writer.skipped = true
  │
  ├─> Writer.Write() (如果未跳过)
  │
  └─> Writer.Commit()
        │
        ├─> callback.RefCountManager.UpdateRefCount() (如果执行了操作)
        │
        └─> client.LockClient.Unlock()
              │
              └─> HTTP POST /unlock
                    │
                    └─> server.Handler.Unlock()
                          │
                          └─> server.LockManager.Unlock()
                                │
                                └─> 只释放锁，不更新引用计数
```

## 需要调整的代码

### 1. Server端：移除引用计数判断

```go
// server/lock_manager.go
func (lm *LockManager) TryLock(request *LockRequest) (bool, bool, string) {
    // 只检查锁状态，不判断引用计数
    // 移除：skip, errMsg := lm.refCountManager.ShouldSkipOperation(...)
    // 移除：canPerform, errMsg := lm.refCountManager.CanPerformOperation(...)
    
    // 只返回锁的获取状态
    return acquired, false, ""
}
```

### 2. Content插件：添加引用计数判断

```go
// content/writer.go
func OpenWriter(ctx context.Context, serverURL, nodeID, resourceID string) (*Writer, error) {
    // 获取锁
    result, err := client.ClusterLock(ctx, writer.client, request)
    if err != nil {
        return nil, err
    }
    
    if result.Acquired {
        // 获取锁后，通过callback包判断是否执行操作
        manager := callback.NewRefCountManager(storage)
        skip, _ := manager.ShouldSkipOperation(callback.OperationTypePull, resourceID)
        if skip {
            writer.skipped = true
            writer.locked = false
        } else {
            writer.locked = true
            writer.skipped = false
        }
    }
    
    return writer, nil
}
```

## 总结

**当前设计**：Server端既管理锁，又判断引用计数
**建议设计**：Server端只管理锁，Content插件通过callback包判断引用计数

**优势**：
1. ✅ 职责清晰：锁管理 vs 业务逻辑
2. ✅ 解耦：Server端不需要了解业务语义
3. ✅ 灵活：不同业务场景可以实现不同的判断逻辑
4. ✅ 独立：Content插件可以独立使用callback包

