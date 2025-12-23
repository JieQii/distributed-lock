# 为什么执行了unlock还没有释放锁？

## 问题现象

**用户问题**：之前下载过的层，执行了unlock，但是锁还没有释放

**日志示例**：
```
[Unlock] 操作成功，保留锁信息: key=pull:sha256:xxx, node=NODEA, 等待队列中的节点通过轮询发现
[TryLock] 操作已完成且成功: key=pull:sha256:xxx, 清理锁
```

## 原因分析

### 服务端的设计逻辑

**位置**：`server/lock_manager.go:153-169`

```go
if lockInfo.Success {
    // 操作成功：保留锁信息（标记为已完成），让队列中的节点通过轮询发现操作已完成
    // 不立即删除锁，也不分配锁给队列中的节点
    // 队列中的节点通过轮询 /lock/status 会发现 completed=true && success=true，从而跳过操作
    // 锁会在 TryLock 中被清理（当发现操作已完成时）
    log.Printf("[Unlock] 操作成功，保留锁信息: key=%s, node=%s, 等待队列中的节点通过轮询发现",
        key, request.NodeID)
    
    // 触发订阅消息广播
    lm.broadcastEvent(shard, key, &OperationEvent{...})
}
```

**关键点**：
- ✅ **操作成功时，锁被保留**（不删除）
- ✅ **锁标记为已完成**：`Completed=true, Success=true`
- ✅ **锁会在 TryLock 中被清理**（当新的请求到来时）

### 锁的清理时机

**位置**：`server/lock_manager.go:79-93`

```go
if lockInfo.Completed {
    if lockInfo.Success {
        log.Printf("[TryLock] 操作已完成且成功: key=%s, 清理锁", key)
    }
    delete(shard.locks, key)  // 清理锁
    return false, false, ""
}
```

**关键点**：
- ✅ **锁在 TryLock 中被清理**（当新的请求到来时）
- ❌ **如果没有新的请求，锁会一直保留**

## 时间线分析

### 场景1：正常流程（有等待节点）

```
T1: 节点A获取锁，开始下载
    → lockInfo = {Completed: false, Success: false}

T2: 节点B请求锁
    → 锁被占用，加入队列
    → 返回 acquired=false
    → 节点B建立 SSE 订阅

T3: 节点A操作完成，调用 Unlock（成功）
    → lockInfo.Completed = true
    → lockInfo.Success = true
    → **保留锁信息，不删除锁** ✅
    → 广播事件给订阅者（节点B）

T4: 节点B通过 SSE 接收事件
    → 收到 Success=true 的事件
    → 处理事件，返回错误提示检查资源 ✅

T5: （可选）节点B再次请求锁
    → TryLock 发现锁已完成
    → 清理锁：delete(shard.locks, key) ✅
```

### 场景2：问题场景（没有等待节点）

```
T1: 节点A获取锁，开始下载
    → lockInfo = {Completed: false, Success: false}

T2: 节点A操作完成，调用 Unlock（成功）
    → lockInfo.Completed = true
    → lockInfo.Success = true
    → **保留锁信息，不删除锁** ✅
    → 广播事件（但没有订阅者）

T3: （一段时间后，没有新的请求）
    → **锁一直保留在内存中** ⚠️

T4: （很久以后）节点B请求锁（新请求）
    → TryLock 发现锁已完成
    → 清理锁：delete(shard.locks, key) ✅
```

## 问题根源

### 问题1：锁会一直保留，直到新的请求到来

**原因**：
- 操作成功时，锁被保留（设计如此）
- 锁只在 TryLock 中被清理（当新的请求到来时）
- **如果没有新的请求，锁会一直保留在内存中**

**影响**：
- ⚠️ **内存泄漏风险**：如果有很多已完成的锁，会占用内存
- ⚠️ **新请求无法跳过操作**：TryLock 没有返回 `skip=true`

### 问题2：新请求无法跳过操作

**当前代码**（`server/lock_manager.go:79-93`）：

```go
if lockInfo.Completed {
    if lockInfo.Success {
        log.Printf("[TryLock] 操作已完成且成功: key=%s, 清理锁", key)
    }
    delete(shard.locks, key)
    return false, false, ""  // ❌ 应该返回 skip=true
}
```

**问题**：
- 新请求到来时，发现锁已完成，清理锁
- 但返回 `acquired=false, skip=false`
- 客户端无法知道应该跳过操作
- 客户端进入等待流程，但锁已经被清理了

## 解决方案

### 方案1：修改 TryLock 返回 skip=true（推荐）

**修改 `server/lock_manager.go:79-93`**：

```go
if lockInfo.Completed {
    if lockInfo.Success {
        log.Printf("[TryLock] 操作已完成且成功: key=%s, 清理锁", key)
        delete(shard.locks, key)
        return false, true, ""  // ✅ 返回 skip=true
    } else {
        log.Printf("[TryLock] 操作已完成但失败: key=%s, 处理队列", key)
        delete(shard.locks, key)
        lm.processQueue(shard, key)
        return false, false, ""
    }
}
```

**优点**：
- ✅ 新请求可以跳过操作
- ✅ 不需要修改客户端代码（如果客户端支持 skip 字段）

### 方案2：增加锁的过期时间（长期优化）

**思路**：
- 操作成功后，锁保留一段时间（如30秒）
- 超过时间后自动清理
- 新的请求在锁过期前可以检测到操作已完成

**实现**：

```go
type LockInfo struct {
    Request     *LockRequest `json:"request"`
    AcquiredAt  time.Time    `json:"acquired_at"`
    Completed   bool         `json:"completed"`
    Success     bool         `json:"success"`
    CompletedAt time.Time    `json:"completed_at"`
    ExpiresAt   time.Time    `json:"expires_at"`  // 新增：过期时间
}

// 在 Unlock 中设置过期时间
if lockInfo.Success {
    lockInfo.ExpiresAt = time.Now().Add(30 * time.Second)
    // 保留锁信息
}

// 在 TryLock 中检查过期
if lockInfo.Completed {
    if lockInfo.Success {
        if time.Now().After(lockInfo.ExpiresAt) {
            // 锁已过期，清理
            delete(shard.locks, key)
            return false, false, ""
        }
        // 锁未过期，返回 skip=true
        return false, true, ""
    }
}
```

### 方案3：定期清理已完成的锁（后台任务）

**思路**：
- 启动一个后台 goroutine
- 定期扫描已完成的锁
- 清理超过一定时间的锁

**实现**：

```go
func (lm *LockManager) StartCleanupRoutine() {
    go func() {
        ticker := time.NewTicker(30 * time.Second)
        defer ticker.Stop()
        
        for range ticker.C {
            lm.cleanupCompletedLocks()
        }
    }()
}

func (lm *LockManager) cleanupCompletedLocks() {
    now := time.Now()
    for _, shard := range lm.shards {
        shard.mu.Lock()
        for key, lockInfo := range shard.locks {
            if lockInfo.Completed && lockInfo.Success {
                // 如果锁已完成超过30秒，清理
                if now.Sub(lockInfo.CompletedAt) > 30*time.Second {
                    delete(shard.locks, key)
                    log.Printf("[Cleanup] 清理已完成的锁: key=%s", key)
                }
            }
        }
        shard.mu.Unlock()
    }
}
```

## 当前行为说明

### 为什么"执行了unlock还没有释放锁"？

**答案**：这是**设计如此**，不是bug

1. **操作成功时，锁被保留**：
   - 目的是让等待队列中的节点发现操作已完成
   - 锁标记为 `Completed=true, Success=true`
   - 锁不会被立即删除

2. **锁的清理时机**：
   - 当新的请求到来时，TryLock 发现锁已完成，清理锁
   - 如果没有新的请求，锁会一直保留

3. **这是正常行为**：
   - ✅ 等待的节点可以通过SSE接收事件
   - ✅ 新请求可以通过 TryLock 发现操作已完成
   - ⚠️ 但新请求无法跳过操作（需要修复）

### 这是服务端的问题吗？

**部分正确**：

1. ✅ **锁被保留是设计如此**：
   - 目的是支持等待队列中的节点发现操作已完成
   - 这是正常的设计行为

2. ❌ **TryLock 没有返回 skip=true 是问题**：
   - 新请求无法跳过操作
   - 需要修复 TryLock 方法

3. ⚠️ **锁会一直保留是潜在问题**：
   - 如果没有新的请求，锁会一直占用内存
   - 建议增加过期时间或定期清理

## 建议

### 立即修复

1. **修改 TryLock 方法**：当发现操作已完成且成功时，返回 `skip=true`
2. **修改客户端处理**：检查 `skip` 字段，如果为 `true`，跳过操作

### 长期优化

1. **增加锁的过期时间**：操作成功后，锁保留30秒后自动清理
2. **定期清理已完成的锁**：启动后台任务，定期清理超过一定时间的锁
3. **监控锁的状态**：统计有多少锁被保留，保留时间多长

## 总结

1. **为什么执行了unlock还没有释放锁**：
   - 操作成功时，锁被保留（设计如此）
   - 锁标记为已完成，但不会被立即删除
   - 锁会在 TryLock 中被清理（当新的请求到来时）

2. **这是服务端的问题吗**：
   - ✅ 锁被保留是设计如此（正常行为）
   - ❌ TryLock 没有返回 `skip=true` 是问题（需要修复）
   - ⚠️ 锁会一直保留是潜在问题（建议优化）

3. **解决方案**：
   - 立即修复：修改 TryLock，返回 `skip=true`
   - 长期优化：增加过期时间或定期清理

