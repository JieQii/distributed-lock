# 仲裁逻辑说明

## 仲裁逻辑概述（按 subtype 队列）

所有操作类型（Pull、Delete、Update）的仲裁逻辑遵循相同的两步检查，并且**等待队列按 subtype 区分**：

1. **第一步：检查是否有其他节点在操作**
   - 若资源已有 holder 且未完成：按 *subtype* 加入对应 waiter 队列（不再是全局 FIFO）
   - 若资源已有 holder 且已完成：根据结果决定是否跳过
   - 若资源无 holder：占位为当前请求并返回 acquired=true

2. **第二步：检查引用计数是否符合预期（锁不存在时，用于判断是否跳过）**
   - Pull：`refcount != 0` → 跳过（已下载完成但未刷新 mergerfs）
   - Delete：`refcount > 0` → 返回错误；`refcount == 0` → 允许
   - Update：是否要求 `refcount == 0` 由配置 `UpdateRequiresNoRef` 决定，不用于跳过

## Pull操作的仲裁逻辑

### 检查步骤

1. **检查是否有其他节点在下载**
   - 如果锁存在且操作未完成 → 按 subtype 加入对应等待队列
   - 如果锁存在且操作已完成 → 根据结果处理

2. **检查引用计数（预期refcount != 0）**
   - 如果 `refcount != 0` → 跳过操作（已下载完成，但还没刷新mergerfs）
   - 如果 `refcount == 0` → 继续执行pull操作

### 逻辑说明

- **refcount != 0**：说明已经有节点下载完成并开始使用该资源，当前节点应该跳过下载
- **refcount == 0**：说明资源还未被下载，当前节点可以执行下载操作

### 代码实现

```go
case OperationTypePull:
    // 如果refcount != 0，说明已经下载完成（但还没刷新mergerfs），应该跳过
    if refCount.Count > 0 {
        return false, true, "" // 跳过操作
    }
    // 引用计数为0，可以继续执行pull操作
```

## Delete操作的仲裁逻辑

### 检查步骤

1. **检查是否有其他节点在删除**
   - 如果锁存在且操作未完成 → 按 subtype 加入对应等待队列
   - 如果锁存在且操作已完成 → 根据结果处理

2. **检查引用计数（预期refcount == 0）**
   - 如果 `refcount > 0` → 返回错误（有节点正在使用，不能删除）
   - 如果 `refcount == 0` → 继续执行delete操作（可能资源已删除但还没刷新mergerfs，或资源不存在）

### 逻辑说明

- **refcount > 0**：说明有节点正在使用该资源，不能执行删除操作
- **refcount == 0**：说明没有节点在使用，可以执行删除操作
  - 可能是资源从未被pull过
  - 可能是资源已经被删除完成（但还没刷新mergerfs）

### 代码实现

```go
case OperationTypeDelete:
    // 如果refcount > 0，不能执行delete操作（有节点在使用）
    if refCount.Count > 0 {
        return false, false, "无法删除：当前有节点正在使用该资源"
    }
    // refcount == 0，可以继续执行delete操作
```

## Update操作的仲裁逻辑

### 检查步骤

1. **检查是否有其他节点在更新**
   - 如果锁存在且操作未完成 → 按 subtype 加入对应等待队列
   - 如果锁存在且操作已完成 → 根据结果处理

2. **检查引用计数（根据配置决定）**
   - 如果配置 `UpdateRequiresNoRef = true` 且 `refcount > 0` → 返回错误（不允许热更新）
   - 如果配置 `UpdateRequiresNoRef = false` → 允许在有引用时更新（热更新）
   - Update操作不基于refcount来决定是否跳过，因为update可能用于创建新资源

### 逻辑说明

- **UpdateRequiresNoRef = true**：要求引用计数为0才能更新（不允许热更新）
- **UpdateRequiresNoRef = false**：允许在有引用时更新（允许热更新，默认）
- Update操作不检查refcount来决定是否跳过，因为：
  - update可能用于创建新资源（refcount == 0）
  - update可能用于更新现有资源（refcount > 0）
- **顺序性**：跨 subtype 不保证全局 FIFO，业务层决定操作顺序；锁仅保证同一资源同一时间只有一个 holder

### 代码实现

```go
case OperationTypeUpdate:
    // 如果配置要求UpdateRequiresNoRef且refcount > 0，不能执行update操作
    if lm.UpdateRequiresNoRef && refCount.Count > 0 {
        return false, false, "无法更新：当前有节点正在使用该资源，不允许更新"
    }
    // 其他情况可以继续执行update操作
```

## 引用计数的作用

引用计数用于判断**操作是否已完成但还没刷新mergerfs**：

- **Pull操作**：如果refcount != 0，说明已经下载完成，应该跳过
- **Delete操作**：如果refcount == 0，说明可能已经删除完成，但为了安全仍然允许执行delete
- **Update操作**：不基于refcount来决定是否跳过

## Callback 与节点信息

- 客户端获得锁后会调用 content 插件中的 callback，callback 接收节点信息和错误信息
- callback 成功/失败都会通过 `/unlock` 上报，附带 Success/错误信息，服务端据此唤醒对应 subtype 队列
- 删除操作只是“解引用”，需要在解锁前将所有 waiter 的 node 信息带给 callback，业务侧决定最终删除与否

## 完整流程图

```
请求到达
  ↓
获取分段锁
  ↓
检查锁是否存在
  ├─ 锁存在且操作未完成 → 加入等待队列 → 返回
  ├─ 锁存在且操作已完成且成功 → 跳过操作 → 返回
  └─ 锁存在且操作已完成但失败 → 处理队列 → 继续
  ↓
锁不存在，检查引用计数
  ├─ Pull: refcount != 0 → 跳过操作
  ├─ Delete: refcount > 0 → 返回错误
  └─ Update: 根据配置检查
  ↓
获取锁，返回成功
```

## 关键点

1. **两步检查**：先检查锁状态，再检查引用计数
2. **预期值不同**：不同操作类型对引用计数的预期不同
3. **跳过逻辑**：引用计数符合预期时跳过操作（Pull和Delete）
4. **错误处理**：引用计数不符合要求时返回错误（Delete和Update的某些配置）

