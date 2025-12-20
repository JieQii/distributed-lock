# 为什么 processQueue 分配锁会导致无法检测到操作已完成？

## 问题分析

### 场景时间线（修复前）

```
T1: 节点A获取锁
    → lockInfo = {Request: nodeA, Completed: false, Success: false}

T2: 节点B请求锁
    → 锁被占用，加入队列
    → 返回 acquired=false
    → 节点B进入 waitForLock() 轮询

T3: 节点A操作完成，释放锁（成功）
    → lockInfo.Completed = true
    → lockInfo.Success = true
    → delete(shard.locks, key)  // 删除锁
    → processQueue()  // 立即处理队列
      → 从队列中取出节点B的请求
      → 创建新的 LockInfo：
        shard.locks[key] = &LockInfo{
            Request:    nodeB,      // 节点B的请求
            Completed: false,      // ❌ 新锁，操作还没开始！
            Success:   false,      // ❌ 新锁，操作还没开始！
        }

T4: 节点B在轮询中查询 /lock/status
    → 服务器返回：
      {
        acquired: true,    // ✅ 锁已经被分配给自己
        completed: false, // ❌ 新锁，操作还没开始
        success: false    // ❌ 新锁，操作还没开始
      }
    → 节点B发现 acquired=true
    → 节点B认为可以开始操作了
    → 返回 acquired=true，节点B继续执行操作 ❌
```

## 关键问题

### processQueue 创建新锁的问题

当 `processQueue` 分配锁给队列中的节点时，它创建了一个**全新的 LockInfo**：

```go
// processQueue 中的代码
shard.locks[key] = &LockInfo{
    Request:    nextRequest,  // 队列中的请求（节点B）
    AcquiredAt: time.Now(),
    Completed:  false,        // ❌ 新锁，操作还没开始！
    Success:    false,         // ❌ 新锁，操作还没开始！
}
```

**问题**：
- 这个新锁的 `Completed = false`（因为节点B的操作还没开始）
- 节点B查询 `/lock/status` 时，看到 `completed=false`
- 节点B无法知道节点A的操作已经完成了

### 节点B的轮询逻辑

在 `waitForLock` 中，节点B的轮询逻辑：

```go
// 查询 /lock/status
statusResp := {acquired: true, completed: false, success: false}

// 检查操作是否已完成
if statusResp.Completed && statusResp.Success {
    // ❌ 这个条件永远不会满足，因为 completed=false
    return skipped=true
}

// 检查是否获得锁
if statusResp.Acquired {
    // ✅ 这个条件满足，因为锁已经被分配给自己
    return acquired=true  // ❌ 节点B继续执行操作，但操作已经完成了！
}
```

## 为什么无法检测到操作已完成？

### 原因1：锁信息被覆盖

当 `processQueue` 分配锁时，它**覆盖了原来的锁信息**：

```
修复前：
T3时刻：节点A释放锁
  → lockInfo = {Completed: true, Success: true}  // 节点A的操作已完成
  → delete(shard.locks, key)  // 删除锁
  → processQueue()  // 立即分配锁给节点B
    → 创建新锁：lockInfo = {Completed: false, Success: false}  // ❌ 新锁，操作还没开始

T4时刻：节点B查询状态
  → 只能看到新锁的信息：{completed: false}
  → 无法知道节点A的操作已经完成了
```

### 原因2：时间窗口问题

即使节点B在轮询中查询，也存在时间窗口：

```
T3.1: 节点A释放锁，设置 Completed=true
T3.2: processQueue() 立即分配锁给节点B（创建新锁，Completed=false）
T3.3: 节点B查询 /lock/status
      → 只能看到新锁（Completed=false）
      → 无法看到节点A的操作已完成
```

**时间窗口太短**：从节点A释放锁到 processQueue 分配锁给节点B，时间间隔几乎为0，节点B无法在这个时间窗口内查询到操作已完成的状态。

### 原因3：轮询间隔

节点B每500ms轮询一次，但：
- 节点A释放锁后，processQueue 立即执行（几乎0延迟）
- 节点B的下一次轮询可能在500ms后
- 到那时，锁已经被分配给节点B了（Completed=false）

## 修复方案

### 修复后的逻辑

```
T3: 节点A操作完成，释放锁（成功）
    → lockInfo.Completed = true
    → lockInfo.Success = true
    → **保留锁信息，不删除锁** ✅
    → **不调用 processQueue()** ✅

T4: 节点B在轮询中查询 /lock/status
    → 服务器返回：
      {
        acquired: false,   // 锁不是节点B持有的
        completed: true,   // ✅ 操作已完成
        success: true      // ✅ 操作成功
      }
    → 节点B发现 completed=true && success=true
    → 返回 skipped=true ✅
    → 节点B跳过操作 ✅
```

### 关键改变

1. **操作成功时，保留锁信息**：
   - 不删除锁
   - 不调用 processQueue
   - 让队列中的节点通过轮询发现操作已完成

2. **操作失败时，分配锁给下一个节点**：
   - 删除锁
   - 调用 processQueue
   - 让队列中的节点继续尝试

## 对比

### 修复前（错误）

```
节点A释放锁（成功）
  ↓
立即删除锁
  ↓
立即分配锁给节点B（新锁，Completed=false）
  ↓
节点B查询状态 → 看到新锁（Completed=false）
  ↓
节点B继续执行操作 ❌（但操作已经完成了！）
```

### 修复后（正确）

```
节点A释放锁（成功）
  ↓
保留锁信息（Completed=true, Success=true）
  ↓
不分配锁给节点B
  ↓
节点B查询状态 → 看到操作已完成（Completed=true, Success=true）
  ↓
节点B跳过操作 ✅
```

## 总结

**为什么 processQueue 分配锁会导致无法检测到操作已完成？**

1. **锁信息被覆盖**：processQueue 创建新锁时，`Completed=false`，覆盖了原来的"操作已完成"状态
2. **时间窗口问题**：从释放锁到分配锁的时间间隔几乎为0，节点B无法在这个窗口内查询到操作已完成
3. **轮询间隔**：节点B每500ms轮询一次，到下次轮询时，锁已经被分配给自己了

**解决方案**：
- 操作成功时，保留锁信息（标记为已完成），不分配锁给队列中的节点
- 让队列中的节点通过轮询发现操作已完成，跳过操作

