# Update操作引用计数检查分析

## 问题：Update操作是否需要保证资源没有被使用？

### 两种策略对比

#### 策略1：允许热更新（默认，推荐）

**特点：**
- Update操作可以在有引用时执行
- 不检查引用计数
- 允许在线更新镜像层

**适用场景：**
- 需要在线更新镜像层内容
- 更新操作不会影响正在使用的节点
- 支持滚动更新

**优点：**
- 灵活性高
- 不需要等待所有节点释放资源
- 支持热更新

**缺点：**
- 更新时可能有节点正在使用旧版本
- 需要确保更新操作的兼容性

#### 策略2：不允许热更新（可选配置）

**特点：**
- Update操作必须引用计数为0才能执行
- 检查引用计数
- 确保更新时没有节点在使用

**适用场景：**
- 更新操作会破坏兼容性
- 需要确保所有节点使用新版本
- 更新操作需要独占资源

**优点：**
- 确保更新时资源未被使用
- 避免版本冲突
- 更安全

**缺点：**
- 需要等待所有节点释放资源
- 可能影响服务可用性
- 灵活性较低

## 实现方案

系统提供了配置选项 `UpdateRequiresNoRef`：

```go
type LockManager struct {
    // ...
    UpdateRequiresNoRef bool // false: 允许热更新（默认）
                            // true: 不允许热更新
}
```

### 默认行为（允许热更新）

```go
lm := NewLockManager()
// lm.UpdateRequiresNoRef = false (默认)

// 即使有节点正在使用资源，update操作也可以执行
updateReq := &LockRequest{
    Type:       OperationTypeUpdate,
    ResourceID: "sha256:abc123",
    NodeID:     "node-1",
}
acquired, _, _ := lm.TryLock(updateReq) // 可以成功
```

### 配置为不允许热更新

```go
lm := NewLockManager()
lm.UpdateRequiresNoRef = true

// 如果有节点正在使用资源，update操作会失败
updateReq := &LockRequest{
    Type:       OperationTypeUpdate,
    ResourceID: "sha256:abc123",
    NodeID:     "node-1",
}
acquired, _, errMsg := lm.TryLock(updateReq)
// errMsg = "无法更新：当前有节点正在使用该资源，不允许更新"
```

## 建议

**推荐使用默认策略（允许热更新）**，原因：

1. **灵活性**：大多数场景下，更新操作不会影响正在使用的节点
2. **可用性**：不需要等待所有节点释放资源，提高系统可用性
3. **性能**：避免不必要的等待和阻塞

**仅在以下情况使用"不允许热更新"策略：**

1. 更新操作会破坏兼容性
2. 需要确保所有节点同时切换到新版本
3. 更新操作需要独占资源（如文件系统操作）

## 代码实现位置

检查逻辑在 `server/lock_manager.go` 的 `TryLock` 方法中：

```go
// 对于update操作，根据配置决定是否需要检查引用计数
if request.Type == OperationTypeUpdate && lm.UpdateRequiresNoRef {
    if refCount.Count > 0 {
        // 配置要求update操作必须引用计数为0
        return false, false, "无法更新：当前有节点正在使用该资源，不允许更新"
    }
}
```

