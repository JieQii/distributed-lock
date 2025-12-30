# 引用计数文件设计说明

## 概述

引用计数文件用于跟踪每个资源（镜像层）被多少个节点使用，用于判断操作是否应该执行。

---

## 核心数据结构

### ReferenceCount

```go
type ReferenceCount struct {
    Count int             // 当前使用该资源的节点数
    Nodes map[string]bool // 使用该资源的节点集合（用于调试和监控）
}
```

**字段说明**：
- `Count`: 使用该资源的节点数量
- `Nodes`: 使用该资源的节点ID集合（map结构，key为节点ID，value为true）

**示例**：
```json
{
  "count": 2,
  "nodes": {
    "node-1": true,
    "node-2": true
  }
}
```

---

## 存储接口设计

### RefCountStorage 接口

```go
type RefCountStorage interface {
    // GetRefCount 获取资源的引用计数
    // 如果不存在，应该创建并初始化为0
    GetRefCount(resourceID string) *ReferenceCount

    // SetRefCount 设置资源的引用计数
    SetRefCount(resourceID string, refCount *ReferenceCount)

    // DeleteRefCount 删除资源的引用计数
    DeleteRefCount(resourceID string)
}
```

**设计特点**：
- ✅ **接口抽象**：支持不同的存储实现（内存、文件、数据库、Redis等）
- ✅ **线程安全**：实现类需要保证并发安全
- ✅ **返回副本**：`GetRefCount` 应该返回副本，避免外部修改原对象

---

## 当前实现：LocalRefCountStorage

### 实现位置

- `content/refcount_storage.go`
- `conchContent-v3/lockintegration/refcount_storage.go`

### 实现方式

```go
type LocalRefCountStorage struct {
    mu        sync.RWMutex
    refCounts map[string]*callback.ReferenceCount
}
```

**特点**：
- ✅ **内存存储**：使用 `map[string]*ReferenceCount` 存储
- ✅ **线程安全**：使用 `sync.RWMutex` 保护并发访问
- ✅ **返回副本**：`GetRefCount` 返回副本，防止外部修改

### 关键实现

#### GetRefCount（获取引用计数）

```go
func (s *LocalRefCountStorage) GetRefCount(resourceID string) *callback.ReferenceCount {
    s.mu.RLock()
    ref, ok := s.refCounts[resourceID]
    s.mu.RUnlock()
    
    if !ok {
        // 不存在时返回初始值
        return &callback.ReferenceCount{
            Count: 0,
            Nodes: map[string]bool{},
        }
    }

    // 返回副本防止外部修改
    nodesCopy := make(map[string]bool)
    for k, v := range ref.Nodes {
        nodesCopy[k] = v
    }
    return &callback.ReferenceCount{
        Count: ref.Count,
        Nodes: nodesCopy,
    }
}
```

**要点**：
- 不存在时返回 `Count: 0`
- 返回副本，避免并发修改

#### SetRefCount（写入引用计数）

```go
func (s *LocalRefCountStorage) SetRefCount(resourceID string, refCount *callback.ReferenceCount) {
    // 创建副本
    nodesCopy := make(map[string]bool)
    for k, v := range refCount.Nodes {
        nodesCopy[k] = v
    }

    s.mu.Lock()
    s.refCounts[resourceID] = &callback.ReferenceCount{
        Count: refCount.Count,
        Nodes: nodesCopy,
    }
    s.mu.Unlock()
}
```

**要点**：
- 存储副本，避免外部修改影响内部数据

#### DeleteRefCount（删除引用计数）

```go
func (s *LocalRefCountStorage) DeleteRefCount(resourceID string) {
    s.mu.Lock()
    delete(s.refCounts, resourceID)
    s.mu.Unlock()
}
```

---

## 引用计数管理器：RefCountManager

### 核心功能

```go
type RefCountManager struct {
    storage RefCountStorage
}
```

### 主要方法

#### 1. UpdateRefCount（更新引用计数）

**规则**：

| 操作类型 | 操作成功 | 操作失败 | 说明 |
|---------|---------|---------|------|
| Pull | 引用计数+1 | 不改变 | 节点开始使用该资源 |
| Update | 不改变 | 不改变 | 更新操作不影响使用状态 |
| Delete | 清理引用计数 | 不改变 | 资源被删除，引用计数信息清理 |

**Pull操作逻辑**：
```go
case OperationTypePull:
    refCount := m.storage.GetRefCount(resourceID)
    if !refCount.Nodes[result.NodeID] {
        // 创建新的引用计数对象
        newRefCount := &ReferenceCount{
            Count: refCount.Count + 1,
            Nodes: make(map[string]bool),
        }
        // 复制原有节点
        for k, v := range refCount.Nodes {
            newRefCount.Nodes[k] = v
        }
        // 添加新节点
        newRefCount.Nodes[result.NodeID] = true
        m.storage.SetRefCount(resourceID, newRefCount)
    }
```

**要点**：
- 只有操作成功时才更新
- 检查节点是否已存在，避免重复计数
- 创建新对象，避免修改原对象

#### 2. ShouldSkipOperation（判断是否应该跳过操作）

**Pull操作**：
```go
case OperationTypePull:
    if refCount.Count > 0 {
        return true, ""  // 跳过操作
    }
```

**Delete操作**：
```go
case OperationTypeDelete:
    if refCount.Count > 0 {
        return false, "无法删除：当前有节点正在使用该资源"
    }
```

**Update操作**：
- 不基于引用计数决定是否跳过

#### 3. CanPerformOperation（判断是否可以执行操作）

**Delete操作**：
```go
case OperationTypeDelete:
    if refCount.Count > 0 {
        return false, "无法删除：当前有节点正在使用该资源"
    }
    return true, ""
```

**Update操作**：
```go
case OperationTypeUpdate:
    if updateRequiresNoRef && refCount.Count > 0 {
        return false, "无法更新：当前有节点正在使用该资源，不允许更新"
    }
    return true, ""
```

---

## 使用流程

### 1. 初始化

```go
// 创建存储实现
storage := NewLocalRefCountStorage()

// 创建管理器
refCountManager := callback.NewRefCountManager(storage)
```

### 2. 操作前判断

```go
// 判断是否应该跳过操作
skip, errMsg := refCountManager.ShouldSkipOperation(
    callback.OperationTypePull, 
    resourceID,
)

if skip {
    // 跳过操作，不请求锁
    return
}
```

### 3. 操作后更新

```go
// 操作成功
result := &callback.OperationResult{
    Success: true,
    NodeID:  nodeID,
}

// 更新引用计数
refCountManager.UpdateRefCount(
    callback.OperationTypePull,
    resourceID,
    result,
)
```

---

## 存储实现建议

### 1. 内存存储（当前实现）

**优点**：
- ✅ 简单快速
- ✅ 适合单节点或测试环境

**缺点**：
- ❌ 数据不持久化，重启后丢失
- ❌ 不适合多节点共享

**适用场景**：
- 单节点环境
- 测试环境
- 临时数据

### 2. 文件存储（推荐用于单节点持久化）

**实现思路**：
```go
type FileRefCountStorage struct {
    filePath string
    refCounts map[string]*ReferenceCount
    mu sync.RWMutex
}

func (s *FileRefCountStorage) GetRefCount(resourceID string) *ReferenceCount {
    // 从文件读取
    // 如果文件不存在，返回 Count: 0
}

func (s *FileRefCountStorage) SetRefCount(resourceID string, refCount *ReferenceCount) {
    // 保存到文件（JSON格式）
    // 可以每个资源一个文件，或所有资源一个文件
}
```

**文件格式建议**：
- **每个资源一个文件**：`refcounts/{resourceID}.json`
- **所有资源一个文件**：`refcounts/all.json`（需要加锁保护）

**优点**：
- ✅ 数据持久化
- ✅ 适合单节点环境
- ✅ 实现简单

**缺点**：
- ❌ 多节点需要共享文件系统（NFS等）
- ❌ 文件锁可能成为瓶颈

### 3. 数据库存储（推荐用于多节点）

**实现思路**：
```go
type DatabaseRefCountStorage struct {
    db *sql.DB
}

func (s *DatabaseRefCountStorage) GetRefCount(resourceID string) *ReferenceCount {
    // 从数据库查询
    // SELECT count, nodes FROM refcounts WHERE resource_id = ?
}

func (s *DatabaseRefCountStorage) SetRefCount(resourceID string, refCount *ReferenceCount) {
    // 保存到数据库
    // INSERT OR REPLACE INTO refcounts (resource_id, count, nodes) VALUES (?, ?, ?)
}
```

**表结构建议**：
```sql
CREATE TABLE refcounts (
    resource_id VARCHAR(255) PRIMARY KEY,
    count INT NOT NULL,
    nodes TEXT,  -- JSON格式存储节点集合
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
```

**优点**：
- ✅ 数据持久化
- ✅ 多节点共享
- ✅ 支持事务

**缺点**：
- ❌ 需要数据库服务
- ❌ 可能有性能瓶颈

### 4. Redis存储（推荐用于分布式环境）

**实现思路**：
```go
type RedisRefCountStorage struct {
    client *redis.Client
}

func (s *RedisRefCountStorage) GetRefCount(resourceID string) *ReferenceCount {
    // 从Redis读取
    // HGET refcounts:{resourceID} count
    // HGET refcounts:{resourceID} nodes
}

func (s *RedisRefCountStorage) SetRefCount(resourceID string, refCount *ReferenceCount) {
    // 保存到Redis
    // HSET refcounts:{resourceID} count {count}
    // HSET refcounts:{resourceID} nodes {nodes_json}
}
```

**Redis键结构**：
- `refcounts:{resourceID}`: Hash结构
  - `count`: 整数
  - `nodes`: JSON字符串

**优点**：
- ✅ 高性能
- ✅ 多节点共享
- ✅ 支持过期时间

**缺点**：
- ❌ 需要Redis服务
- ❌ 数据可能丢失（取决于持久化配置）

---

## 设计优势

### 1. 接口抽象

- ✅ 支持不同的存储实现
- ✅ 易于测试和替换
- ✅ 符合依赖倒置原则

### 2. 线程安全

- ✅ 所有实现都保证并发安全
- ✅ 使用锁保护共享数据

### 3. 数据隔离

- ✅ `GetRefCount` 返回副本，避免外部修改
- ✅ `SetRefCount` 存储副本，避免外部修改影响内部数据

### 4. 职责分离

- ✅ 存储层：只负责数据的读写
- ✅ 管理层：负责业务逻辑（更新规则、判断逻辑）

---

## 当前设计总结

### 存储方式

- **当前实现**：内存存储（`LocalRefCountStorage`）
- **存储位置**：`content/refcount_storage.go`
- **数据结构**：`map[string]*ReferenceCount`

### 数据持久化

- ❌ **当前不支持持久化**：数据存储在内存中，重启后丢失
- ✅ **设计支持扩展**：可以通过实现 `RefCountStorage` 接口支持文件/数据库等持久化

### 多节点共享

- ❌ **当前不支持多节点共享**：每个节点有独立的引用计数
- ✅ **设计支持扩展**：可以通过实现 `RefCountStorage` 接口支持数据库/Redis等共享存储

### 使用场景

- ✅ **单节点环境**：当前实现完全满足
- ✅ **测试环境**：内存存储简单快速
- ⚠️ **生产环境**：建议实现文件或数据库存储，保证数据持久化

---

## 后续改进建议

### 1. 实现文件存储

- 每个资源一个文件：`refcounts/{resourceID}.json`
- 支持启动时加载，运行时保存

### 2. 实现数据库存储

- 使用SQLite（单节点）或PostgreSQL/MySQL（多节点）
- 支持事务，保证数据一致性

### 3. 实现Redis存储

- 适合分布式环境
- 高性能，支持过期时间

### 4. 添加配置选项

- 支持通过配置文件选择存储方式
- 支持存储路径、连接信息等配置

---

## 相关文件

- **接口定义**：`callback/types.go`
- **管理器实现**：`callback/manager.go`
- **当前存储实现**：`content/refcount_storage.go`
- **使用示例**：`content/writer.go`

