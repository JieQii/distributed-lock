# Callback 包架构说明

## 概述

Callback 包将引用计数相关的逻辑从 server 端分离出来，使得 Content 插件可以直接使用引用计数功能，而不需要依赖 server 端。

## 架构设计

```
┌─────────────────────────────────────────────────────────┐
│                    Content 插件                         │
│  ┌─────────────────────────────────────────────────┐   │
│  │        可以直接使用 callback 包                  │   │
│  │  - RefCountManager                              │   │
│  │  - 自定义存储实现                                │   │
│  └─────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────┘
                          │
                          │ 使用
                          ▼
┌─────────────────────────────────────────────────────────┐
│                  callback 包                            │
│  ┌─────────────────────────────────────────────────┐   │
│  │  RefCountManager (核心逻辑)                      │   │
│  │  - UpdateRefCount                                │   │
│  │  - ShouldSkipOperation                           │   │
│  │  - CanPerformOperation                           │   │
│  └─────────────────────────────────────────────────┘   │
│  ┌─────────────────────────────────────────────────┐   │
│  │  RefCountStorage (接口)                         │   │
│  │  - GetRefCount                                   │   │
│  │  - SetRefCount                                   │   │
│  │  - DeleteRefCount                                │   │
│  └─────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────┘
                          ▲
                          │ 实现
                          │
        ┌─────────────────┴─────────────────┐
        │                                     │
┌───────┴────────┐                  ┌────────┴────────┐
│  Server 端     │                  │  Content 插件   │
│  refCountStorage│                  │  自定义存储      │
│  (适配器)      │                  │  (可选)          │
└────────────────┘                  └─────────────────┘
```

## 核心组件

### 1. RefCountStorage 接口

定义引用计数的存储接口，允许不同的实现：

```go
type RefCountStorage interface {
    GetRefCount(resourceID string) *ReferenceCount
    SetRefCount(resourceID string, refCount *ReferenceCount)
    DeleteRefCount(resourceID string)
}
```

**实现方式**：
- **Server 端**：通过 `refCountStorage` 适配器，连接到 server 的 shard 存储
- **Content 插件**：可以实现自定义存储（内存、文件、数据库等）

### 2. RefCountManager

引用计数管理器，提供核心功能：

- **UpdateRefCount**: 根据操作结果更新引用计数
- **GetRefCount**: 获取资源的引用计数
- **ShouldSkipOperation**: 判断是否应该跳过操作
- **CanPerformOperation**: 判断是否可以执行操作

### 3. OperationResult

操作结果结构，用于更新引用计数：

```go
type OperationResult struct {
    Success bool   // 操作是否成功
    NodeID  string // 节点ID
    Error   error  // 错误信息（如果有）
}
```

## 使用场景

### 场景1：Server 端使用（当前实现）

Server 端通过适配器使用 callback 包：

```go
// 创建存储适配器
storage := newRefCountStorage(lm.shards)

// 创建管理器
lm.refCountManager = callback.NewRefCountManager(storage)

// 在 TryLock 中使用
skip, errMsg := lm.refCountManager.ShouldSkipOperation(request.Type, request.ResourceID)

// 在 Unlock 中使用
result := &callback.OperationResult{
    Success: request.Success,
    NodeID:  request.NodeID,
}
lm.refCountManager.UpdateRefCount(request.Type, request.ResourceID, result)
```

### 场景2：Content 插件独立使用

Content 插件可以实现自己的存储，直接使用 callback 包：

```go
// 创建自定义存储
storage := &MyStorage{...}

// 创建管理器
manager := callback.NewRefCountManager(storage)

// 判断是否应该跳过操作
skip, _ := manager.ShouldSkipOperation(callback.OperationTypePull, resourceID)
if skip {
    // 跳过操作
    return
}

// 执行操作后更新引用计数
result := &callback.OperationResult{
    Success: true,
    NodeID:  nodeID,
}
manager.UpdateRefCount(callback.OperationTypePull, resourceID, result)
```

## 优势

1. **解耦**：引用计数逻辑与锁管理逻辑分离
2. **可复用**：Content 插件可以直接使用，不需要依赖 server 端
3. **可扩展**：可以轻松实现不同的存储后端
4. **可测试**：可以独立测试引用计数逻辑
5. **灵活性**：不同场景可以使用不同的存储实现

## 迁移说明

### 从 Server 端迁移

原来的引用计数逻辑在 `lock_manager.go` 中：
- `getRefCount`: 已迁移到 callback 包
- `updateRefCount`: 已迁移到 callback 包

现在通过 `refCountManager` 使用 callback 包的功能。

### Content 插件使用

Content 插件现在可以：
1. 直接导入 `callback` 包
2. 实现 `RefCountStorage` 接口
3. 使用 `RefCountManager` 管理引用计数

## 注意事项

1. **存储实现**：实现 `RefCountStorage` 接口时，需要注意线程安全性
2. **数据一致性**：如果使用分布式存储，需要保证数据一致性
3. **性能考虑**：存储实现应该考虑性能，避免成为瓶颈

