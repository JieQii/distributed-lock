# Client库使用方式对比

## 同事的代码（content插件）

### 代码结构

```go
type Store struct {
    // ...
    nodeID string
    lockClient *client.LockClient  // 从外部传入，一个节点一个客户端
}

func NewStore(hostRoot, mergedRoot, nodeID string, lockClient *client.LockClient) (*Store, error) {
    return &Store{
        // ...
        nodeID: nodeID,
        lockClient: lockClient,  // 使用外部传入的客户端
    }, nil
}

func (s *Store) Writer(ctx context.Context, opts ...content.WriterOpt) (content.Writer, error) {
    // 每次调用Writer时，创建新的request
    req := &client.Request{
        Type:       client.OperationTypePull,
        ResourceID: resourceID,
        NodeID:     s.nodeID,  // 使用Store的nodeID
    }
    
    // 使用同一个lockClient实例
    result, err := client.ClusterLock(ctx, s.lockClient, req)
    // ...
}
```

### 特点

1. **一个节点一个客户端**：`lockClient` 从外部传入，在应用启动时创建
2. **共享使用**：所有 `Writer` 操作都使用同一个 `lockClient` 实例
3. **每次创建新request**：每次调用 `Writer` 时创建新的 `req`，但使用同一个 `lockClient`

## 我的测试代码

### 代码结构

```go
// 创建节点A的client
clientA := client.NewLockClient(serverURL, "NODEA")

// 创建节点B的client
clientB := client.NewLockClient(serverURL, "NODEB")

// 每个节点使用自己的客户端
go processLayer(ctx, clientA, "NODEA", layer.ID, layer.Duration, &wg)
go processLayer(ctx, clientB, "NODEB", layer.ID, layer.Duration, &wg)
```

### 特点

1. **一个节点一个客户端**：每个节点创建自己的客户端实例 ✅
2. **独立使用**：每个节点使用自己的客户端
3. **每次创建新request**：每次调用 `Lock` 时创建新的 `req`

## 关键差异分析

### 1. 客户端创建时机

**同事的代码**：
- 客户端在应用启动时创建（外部传入）
- 一个节点只有一个客户端实例
- 所有操作共享这个客户端

**我的测试代码**：
- 客户端在测试开始时创建
- 一个节点只有一个客户端实例
- 所有操作共享这个客户端

**结论**：✅ **一致** - 都是一个节点一个客户端

### 2. NodeID的设置

**同事的代码**：
```go
req := &client.Request{
    NodeID: s.nodeID,  // 手动设置
}
result, err := client.ClusterLock(ctx, s.lockClient, req)
```

**我的测试代码**：
```go
request := &client.Request{
    NodeID: nodeID,  // 手动设置
}
result, err := lockClient.Lock(ctx, request)
```

**关键发现**：`client.Lock()` 方法会**覆盖** `request.NodeID`：

```go
func (c *LockClient) Lock(ctx context.Context, request *Request) (*LockResult, error) {
    // 设置节点ID（覆盖request中的NodeID）
    request.NodeID = c.NodeID
    // ...
}
```

**结论**：⚠️ **需要注意** - `request.NodeID` 会被 `c.NodeID` 覆盖，所以：
- 传入的 `request.NodeID` 会被忽略
- 实际使用的是 `lockClient.NodeID`

### 3. 并发安全性

**同事的代码**：
- `lockClient` 是共享的，但 `Lock()` 方法会修改 `request.NodeID`
- 如果多个goroutine同时使用同一个 `lockClient` 和同一个 `request`，会有问题
- 但同事的代码每次创建新的 `req`，所以是安全的 ✅

**我的测试代码**：
- 每个节点使用自己的 `lockClient`
- 每次调用创建新的 `request`
- 完全安全 ✅

## 潜在问题

### 问题1：NodeID覆盖

如果同事的代码中，`request.NodeID` 和 `lockClient.NodeID` 不一致：

```go
req := &client.Request{
    NodeID: "DIFFERENT_NODE",  // 与lockClient.NodeID不同
}
result, err := client.ClusterLock(ctx, s.lockClient, req)
// 实际使用的NodeID是lockClient.NodeID，不是req.NodeID！
```

**解决方案**：
1. 确保 `request.NodeID` 和 `lockClient.NodeID` 一致
2. 或者修改 `client.Lock()` 方法，不覆盖 `request.NodeID`（如果已设置）

### 问题2：客户端复用

**同事的代码**：
- ✅ 正确：一个节点一个客户端，所有操作共享
- ✅ 高效：避免重复创建客户端

**我的测试代码**：
- ✅ 正确：一个节点一个客户端，所有操作共享
- ✅ 高效：避免重复创建客户端

## 总结

### 相同点

1. ✅ **一个节点一个客户端**：都是正确的设计
2. ✅ **客户端复用**：所有操作共享同一个客户端实例
3. ✅ **每次创建新request**：避免并发问题

### 不同点

1. ⚠️ **NodeID设置**：
   - 同事的代码：手动设置 `request.NodeID`，但会被 `lockClient.NodeID` 覆盖
   - 我的测试代码：手动设置 `request.NodeID`，但会被 `lockClient.NodeID` 覆盖
   - **结论**：都需要确保 `request.NodeID` 和 `lockClient.NodeID` 一致

### 建议

1. **确保NodeID一致**：
   ```go
   // 推荐：不设置request.NodeID，让Lock方法自动设置
   req := &client.Request{
       Type:       client.OperationTypePull,
       ResourceID: resourceID,
       // NodeID: 不设置，使用lockClient.NodeID
   }
   ```

2. **或者修改client.Lock()方法**：
   ```go
   func (c *LockClient) Lock(ctx context.Context, request *Request) (*LockResult, error) {
       // 只在未设置时才设置NodeID
       if request.NodeID == "" {
           request.NodeID = c.NodeID
       }
       // ...
   }
   ```

## 结论

**同事的代码和我的测试代码在使用方式上是一致的**：
- ✅ 都是一个节点一个客户端
- ✅ 都是客户端复用
- ✅ 都是每次创建新的request

**唯一需要注意的**：
- ⚠️ `request.NodeID` 会被 `lockClient.NodeID` 覆盖
- ✅ 只要确保 `lockClient` 的 `NodeID` 正确，就没问题


