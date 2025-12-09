# 操作类型和引用计数机制

## 操作类型

系统支持三种操作类型：

### 1. pull（拉取镜像层）
- **用途**：从镜像仓库拉取镜像层到本地
- **引用计数**：操作成功后，引用计数+1（节点开始使用该资源）
- **锁机制**：与其他操作类型共享同一资源的锁

### 2. update（更新镜像层）
- **用途**：更新已存在的镜像层
- **引用计数**：不改变引用计数
- **锁机制**：与其他操作类型共享同一资源的锁

### 3. delete（删除镜像层）
- **用途**：删除镜像层
- **引用计数**：**必须为0才能执行删除操作**
- **锁机制**：与其他操作类型共享同一资源的锁
- **特殊检查**：在获取锁之前，会检查引用计数是否为0

## 引用计数机制

### 引用计数的作用

引用计数用于跟踪有多少个节点正在使用某个镜像层资源。这对于delete操作至关重要：
- **delete操作要求**：只有当引用计数为0时，才能执行delete操作
- **防止误删**：确保没有节点正在使用该资源时才能删除

### 引用计数的更新规则

| 操作类型 | 操作成功 | 操作失败 | 说明 |
|---------|---------|---------|------|
| pull | 引用计数+1 | 不改变 | 节点开始使用该资源 |
| update | 不改变 | 不改变 | 更新操作不影响使用状态 |
| delete | 清理引用计数 | 不改变 | 资源被删除，引用计数信息清理 |

### 引用计数的存储

- **位置**：每个分段（shard）维护自己的引用计数map
- **结构**：`resourceID -> ReferenceCount`
- **ReferenceCount包含**：
  - `Count`：使用该资源的节点数
  - `Nodes`：使用该资源的节点集合（用于调试和监控）

## 锁的仲裁机制

### 统一锁机制

**重要原则**：不论是什么操作类型（pull、update、delete），对于锁的仲裁而言，都是一个请求锁的申请。

这意味着：
- 同一资源的不同操作类型会竞争同一个锁
- FIFO队列管理所有操作类型的请求
- 锁的获取和释放逻辑对所有操作类型一致

### 锁的唯一标识

锁的唯一标识由 `Type + ResourceID` 组成：
```
key = Type + ":" + ResourceID
```

例如：
- `pull:sha256:abc123...` - pull操作的锁
- `update:sha256:abc123...` - update操作的锁
- `delete:sha256:abc123...` - delete操作的锁

**注意**：不同操作类型对同一资源的锁是独立的，它们不会互相阻塞。

### delete操作的额外检查

虽然锁机制统一，但delete操作在获取锁之前会进行额外的检查：

```go
if request.Type == OperationTypeDelete {
    refCount := lm.getRefCount(shard, request.ResourceID)
    if refCount.Count > 0 {
        // 引用计数不为0，不能执行delete操作
        return false, false, "无法删除：当前有节点正在使用该资源"
    }
}
```

**检查时机**：在尝试获取锁之前
**检查结果**：如果引用计数不为0，直接返回错误，不获取锁，不加入等待队列

## 使用示例

### pull操作

```go
request := &client.Request{
    Type:       client.OperationTypePull,
    ResourceID: "sha256:abc123...",
    NodeID:     "node-1",
}

result, err := client.ClusterLock(ctx, lockClient, request)
if err != nil {
    // 处理错误
}
if result.Error != nil {
    // 处理锁获取错误
}
if result.Skipped {
    // 操作已完成，跳过
}
if result.Acquired {
    // 获得锁，执行pull操作
    // ... 执行pull ...
    // 操作成功后，引用计数自动+1
}
```

### delete操作

```go
request := &client.Request{
    Type:       client.OperationTypeDelete,
    ResourceID: "sha256:abc123...",
    NodeID:     "node-1",
}

result, err := client.ClusterLock(ctx, lockClient, request)
if err != nil {
    // 处理错误
}
if result.Error != nil {
    // 可能是引用计数不为0
    // 错误信息："无法删除：当前有节点正在使用该资源"
}
if result.Acquired {
    // 获得锁，执行delete操作
    // ... 执行delete ...
    // 操作成功后，引用计数信息被清理
}
```

## API接口

### 获取引用计数

```
GET /refcount?resource_id=sha256:abc123...
```

响应：
```json
{
  "resource_id": "sha256:abc123...",
  "count": 3,
  "nodes": {
    "node-1": true,
    "node-2": true,
    "node-3": true
  }
}
```

## 节点断开连接处理

当节点断开连接时，需要清理该节点对资源的所有引用：

```go
lockManager.ReleaseNodeRefs(nodeID)
```

这会：
1. 遍历所有分段
2. 清理该节点在所有资源上的引用
3. 更新引用计数
4. 如果引用计数为0，清理引用计数条目

**建议**：在实际应用中，应该实现节点心跳机制，当节点长时间无响应时，自动调用 `ReleaseNodeRefs` 清理引用。

## 注意事项

1. **引用计数的持久化**：当前实现中，引用计数存储在内存中。如果服务端重启，引用计数会丢失。如果需要持久化，可以考虑：
   - 将引用计数信息写入镜像层的元数据文件
   - 使用外部存储（如Redis、数据库）存储引用计数

2. **计数文件的写入**：用户提到"计数文件写入镜像层中"，这应该在pull/update/delete操作的具体实现中完成，锁管理器只负责维护内存中的引用计数。

3. **并发安全**：引用计数的更新都在分段锁的保护下进行，确保并发安全。

4. **delete操作的原子性**：delete操作需要同时满足两个条件：
   - 获得锁
   - 引用计数为0
   这两个检查都在同一个锁保护下进行，确保原子性。

