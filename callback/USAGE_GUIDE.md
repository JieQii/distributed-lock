# Callback 包使用指南

## 概述

Callback 包提供了引用计数管理的核心功能，**完全独立于 server 端**。Content 插件可以直接使用 callback 包，而不需要依赖 server 端。

## 核心优势

1. **完全解耦**：引用计数逻辑与锁管理逻辑分离
2. **独立使用**：Content 插件可以直接使用，不需要依赖 server 端
3. **灵活存储**：可以实现不同的存储后端（内存、文件、数据库、Redis等）
4. **易于测试**：可以独立测试引用计数逻辑

## 架构说明

```
┌─────────────────────────────────────────┐
│         Content 插件                     │
│  ┌───────────────────────────────────┐  │
│  │  直接使用 callback 包              │  │
│  │  - RefCountManager                │  │
│  │  - 自定义存储实现                  │  │
│  └───────────────────────────────────┘  │
└─────────────────────────────────────────┘
              │
              │ 使用
              ▼
┌─────────────────────────────────────────┐
│         callback 包                      │
│  - RefCountManager (核心逻辑)            │
│  - RefCountStorage (接口)               │
│  - ReferenceCount (类型)                │
└─────────────────────────────────────────┘
```

## 快速开始

### 1. 实现存储接口

首先，实现 `RefCountStorage` 接口：

```go
package content

import "distributed-lock/callback"

// MyStorage 自定义存储实现
type MyStorage struct {
    refCounts map[string]*callback.ReferenceCount
    mu        sync.RWMutex
}

func (s *MyStorage) GetRefCount(resourceID string) *callback.ReferenceCount {
    s.mu.RLock()
    defer s.mu.RUnlock()
    
    refCount, exists := s.refCounts[resourceID]
    if !exists {
        return &callback.ReferenceCount{
            Count: 0,
            Nodes: make(map[string]bool),
        }
    }
    
    // 返回副本，避免并发修改
    nodesCopy := make(map[string]bool)
    for k, v := range refCount.Nodes {
        nodesCopy[k] = v
    }
    return &callback.ReferenceCount{
        Count: refCount.Count,
        Nodes: nodesCopy,
    }
}

func (s *MyStorage) SetRefCount(resourceID string, refCount *callback.ReferenceCount) {
    s.mu.Lock()
    defer s.mu.Unlock()
    
    // 保存副本，避免并发修改
    nodesCopy := make(map[string]bool)
    for k, v := range refCount.Nodes {
        nodesCopy[k] = v
    }
    s.refCounts[resourceID] = &callback.ReferenceCount{
        Count: refCount.Count,
        Nodes: nodesCopy,
    }
}

func (s *MyStorage) DeleteRefCount(resourceID string) {
    s.mu.Lock()
    defer s.mu.Unlock()
    delete(s.refCounts, resourceID)
}
```

### 2. 创建管理器

```go
import "distributed-lock/callback"

// 创建存储
storage := &MyStorage{
    refCounts: make(map[string]*callback.ReferenceCount),
}

// 创建管理器
manager := callback.NewRefCountManager(storage)
```

### 3. 使用管理器

#### 判断是否应该跳过操作

```go
// 判断是否应该跳过Pull操作
skip, errMsg := manager.ShouldSkipOperation(callback.OperationTypePull, resourceID)
if skip {
    // 跳过操作（引用计数 > 0，说明已经下载完成）
    return
}
```

#### 更新引用计数

```go
// 操作成功后，更新引用计数
result := &callback.OperationResult{
    Success: true,
    NodeID:  nodeID,
}
manager.UpdateRefCount(callback.OperationTypePull, resourceID, result)
```

#### 检查是否可以执行操作

```go
// 检查是否可以执行Delete操作
canDelete, errMsg := manager.CanPerformOperation(
    callback.OperationTypeDelete, 
    resourceID, 
    false, // updateRequiresNoRef（对Delete不适用）
)
if !canDelete {
    // 不能执行操作（有节点正在使用资源）
    return fmt.Errorf("无法删除: %s", errMsg)
}
```

## 完整示例

### 场景1：Content插件独立使用（不依赖server端）

```go
package content

import (
    "context"
    "distributed-lock/callback"
)

func PullLayer(ctx context.Context, resourceID, nodeID string) error {
    // 创建存储和管理器
    storage := &MyStorage{...}
    manager := callback.NewRefCountManager(storage)
    
    // 判断是否应该跳过
    skip, _ := manager.ShouldSkipOperation(callback.OperationTypePull, resourceID)
    if skip {
        // 已经下载完成，跳过操作
        return nil
    }
    
    // 执行下载操作
    err := downloadLayer(resourceID)
    if err != nil {
        return err
    }
    
    // 更新引用计数
    result := &callback.OperationResult{
        Success: true,
        NodeID:  nodeID,
    }
    manager.UpdateRefCount(callback.OperationTypePull, resourceID, result)
    
    return nil
}
```

### 场景2：与Server端配合使用

如果使用server端的分布式锁，引用计数由server端管理，content插件只需要：

```go
// 通过锁服务获取操作结果
result, err := client.Lock(ctx, request)
if result.Skipped {
    // Server端已经判断应该跳过
    return nil
}

// 执行操作后，通过Unlock通知server端更新引用计数
client.Unlock(ctx, unlockRequest)
```

## 操作类型

- `callback.OperationTypePull`: 拉取镜像层
- `callback.OperationTypeUpdate`: 更新镜像层
- `callback.OperationTypeDelete`: 删除镜像层

## 引用计数规则

| 操作类型 | 操作成功 | 操作失败 | 说明 |
|---------|---------|---------|------|
| Pull | 引用计数+1 | 不改变 | 节点开始使用该资源 |
| Update | 不改变 | 不改变 | 更新操作不影响使用状态 |
| Delete | 清理引用计数 | 不改变 | 资源被删除，引用计数信息清理 |

## 判断逻辑

### Pull 操作
- `refcount != 0` → 跳过操作（已下载完成）
- `refcount == 0` → 继续执行

### Delete 操作
- `refcount > 0` → 返回错误（有节点在使用）
- `refcount == 0` → 允许执行

### Update 操作
- 根据配置 `updateRequiresNoRef` 决定
- `updateRequiresNoRef = true` 且 `refcount > 0` → 返回错误
- 其他情况 → 允许执行

## 存储实现建议

### 内存存储（适合单节点）

```go
type MemoryStorage struct {
    refCounts map[string]*callback.ReferenceCount
    mu        sync.RWMutex
}
```

### 文件存储（适合单节点持久化）

```go
type FileStorage struct {
    filePath string
    refCounts map[string]*callback.ReferenceCount
    mu        sync.RWMutex
}

// 实现时，需要从文件加载和保存到文件
```

### 数据库存储（适合多节点）

```go
type DatabaseStorage struct {
    db *sql.DB
}

// 实现时，使用数据库表存储引用计数
```

### Redis存储（适合分布式环境）

```go
type RedisStorage struct {
    client *redis.Client
}

// 实现时，使用Redis存储引用计数
```

## 注意事项

1. **线程安全**：实现 `RefCountStorage` 接口时，必须保证线程安全
2. **数据一致性**：如果使用分布式存储，需要保证数据一致性
3. **性能考虑**：存储实现应该考虑性能，避免成为瓶颈
4. **返回副本**：`GetRefCount` 应该返回副本，避免并发修改原对象

## 与Server端的关系

- **Server端**：通过 `refCountStorage` 适配器使用 callback 包，管理分布式环境下的引用计数
- **Content插件**：可以直接使用 callback 包，实现自己的存储，或者与server端配合使用

两种方式可以共存，根据实际需求选择。

