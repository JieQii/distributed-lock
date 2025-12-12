# Callback 包

Callback 包提供了引用计数管理的核心功能，用于判断操作是否应该执行以及更新引用计数。

## 设计目标

将引用计数相关的逻辑从 server 端分离出来，使得：
1. **Content 插件可以直接使用**：Content 插件可以独立使用 callback 包的功能，**不需要依赖 server 端**
2. **职责分离**：引用计数逻辑独立于锁管理逻辑
3. **可扩展性**：可以轻松实现不同的存储后端（内存、文件、数据库、Redis等）
4. **完全解耦**：callback 包不依赖 server 端，可以独立使用

## 核心组件

### 1. ReferenceCount 类型

```go
type ReferenceCount struct {
    Count int            // 当前使用该资源的节点数
    Nodes map[string]bool // 使用该资源的节点集合
}
```

### 2. RefCountStorage 接口

定义引用计数的存储接口，实现此接口可以提供不同的存储后端：

```go
type RefCountStorage interface {
    GetRefCount(resourceID string) *ReferenceCount
    SetRefCount(resourceID string, refCount *ReferenceCount)
    DeleteRefCount(resourceID string)
}
```

### 3. RefCountManager

引用计数管理器，提供核心功能：

- `UpdateRefCount`: 根据操作结果更新引用计数
- `GetRefCount`: 获取资源的引用计数
- `ShouldSkipOperation`: 判断是否应该跳过操作
- `CanPerformOperation`: 判断是否可以执行操作

## 使用方式

### 在 Server 端使用

Server 端实现了 `RefCountStorage` 接口，通过 `refCountStorage` 适配器连接：

```go
// 创建存储适配器
storage := newRefCountStorage(lm.shards)

// 创建管理器
lm.refCountManager = callback.NewRefCountManager(storage)

// 使用管理器
skip, errMsg := lm.refCountManager.ShouldSkipOperation(operationType, resourceID)
```

### 在 Content 插件中使用

Content 插件可以直接使用 callback 包：

```go
import "distributed-lock/callback"

// 创建自定义存储（例如：基于本地文件或数据库）
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

## 操作类型

- `OperationTypePull`: 拉取镜像层
- `OperationTypeUpdate`: 更新镜像层
- `OperationTypeDelete`: 删除镜像层

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

## 实现自定义存储

实现 `RefCountStorage` 接口即可：

```go
type MyStorage struct {
    // 你的存储实现
}

func (s *MyStorage) GetRefCount(resourceID string) *callback.ReferenceCount {
    // 从你的存储中获取
}

func (s *MyStorage) SetRefCount(resourceID string, refCount *callback.ReferenceCount) {
    // 保存到你的存储
}

func (s *MyStorage) DeleteRefCount(resourceID string) {
    // 从你的存储中删除
}
```

## 优势

1. **解耦**：引用计数逻辑与锁管理逻辑分离
2. **可测试**：可以独立测试引用计数逻辑
3. **可扩展**：可以轻松实现不同的存储后端（内存、数据库、Redis等）
4. **可复用**：Content 插件可以直接使用，不需要依赖 server 端

