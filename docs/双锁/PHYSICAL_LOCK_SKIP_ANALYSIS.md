# 物理锁方案下的"操作成功后跳过"机制分析

## 需求分析

### 核心需求

1. **操作成功后的跳过**：
   - 第一个节点下载成功后，后续节点就不用尝试了
   - 后续节点应该能够发现操作已完成，跳过下载

2. **操作失败后的队列处理**：
   - 如果下载失败，才需要给等待队列里的下一个节点
   - 不同的操作类型（pull、update、delete）有不同的等待队列

### 关键问题

使用物理锁（分段锁 + 资源锁）后，如何实现：
1. 操作成功时，如何让后续节点知道应该跳过？
2. 操作失败时，如何将锁分配给等待队列的下一个节点？
3. 如何区分不同操作类型的等待队列？

## 方案设计

### 方案A：保留操作状态 + SSE事件通知（推荐）

#### 设计思路

即使使用物理锁，仍然需要保留操作状态信息，用于：
1. 判断操作是否已完成
2. 判断操作是否成功
3. 通知等待队列中的节点

#### 数据结构

```go
type resourceLock struct {
    mu sync.Mutex  // 资源锁（真实锁）
    
    // 锁信息（保留操作状态）
    lockInfo *LockInfo  // 包含 Completed、Success 字段
    
    // 等待队列：按操作类型分组
    queues map[string][]*LockRequest  // key: operationType, value: FIFO队列
    
    // 订阅者
    subscribers []Subscriber
}
```

#### 关键设计点

1. **操作状态保留**：
   - 即使操作完成，也保留 `lockInfo` 一段时间
   - 或者使用单独的状态存储

2. **操作类型分组的等待队列**：
   - `queues["pull"]` - pull 操作的等待队列
   - `queues["update"]` - update 操作的等待队列
   - `queues["delete"]` - delete 操作的等待队列

3. **操作成功后的处理**：
   - 不分配锁给等待队列
   - 通过 SSE 事件通知所有订阅者
   - 等待队列中的节点收到事件后，检查操作状态，决定跳过

4. **操作失败后的处理**：
   - 从对应操作类型的等待队列中取出下一个节点
   - 分配锁给该节点
   - 通过 SSE 通知该节点

#### 实现流程

##### TryLock 流程

```go
func (lm *LockManager) TryLock(request *LockRequest) (bool, bool, string) {
    key := LockKey(request.Type, request.ResourceID)
    shard := lm.getShard(key)
    
    // 1. 获取分段锁（用于创建/访问资源锁）
    shard.mu.Lock()
    rl := shard.getOrCreateResourceLock(key)
    shard.mu.Unlock()
    
    // 2. 获取资源锁
    rl.mu.Lock()
    defer rl.mu.Unlock()
    
    // 3. 检查操作状态
    if rl.lockInfo != nil {
        if rl.lockInfo.Completed {
            if rl.lockInfo.Success {
                // 操作已完成且成功 → 返回 skip=true
                return false, true, ""  // acquired=false, skip=true
            } else {
                // 操作已完成但失败 → 处理队列
                // 从对应操作类型的队列中取出下一个节点
                nextRequest := lm.getNextFromQueue(rl, request.Type)
                if nextRequest != nil {
                    // 分配锁给下一个节点
                    rl.lockInfo = &LockInfo{
                        Request:    nextRequest,
                        AcquiredAt: time.Now(),
                        Completed:  false,
                        Success:    false,
                    }
                    // 通知该节点
                    lm.notifyLockAssigned(rl, nextRequest.NodeID)
                    // 如果当前请求就是下一个节点，返回 acquired=true
                    if nextRequest.NodeID == request.NodeID {
                        return true, false, ""
                    }
                }
            }
        } else {
            // 操作未完成，检查是否是同一节点重新请求
            if rl.lockInfo.Request.NodeID == request.NodeID {
                // 同一节点重新请求，更新锁信息
                rl.lockInfo.Request = request
                rl.lockInfo.AcquiredAt = time.Now()
                return true, false, ""
            }
            // 其他节点持有锁，加入对应操作类型的等待队列
            lm.addToQueue(rl, request.Type, request)
            return false, false, ""
        }
    }
    
    // 4. 资源锁不存在，创建新的资源锁
    rl.lockInfo = &LockInfo{
        Request:    request,
        AcquiredAt: time.Now(),
        Completed:  false,
        Success:    false,
    }
    
    return true, false, ""
}
```

##### Unlock 流程

```go
func (lm *LockManager) Unlock(request *UnlockRequest) bool {
    key := LockKey(request.Type, request.ResourceID)
    shard := lm.getShard(key)
    
    // 1. 获取分段锁
    shard.mu.Lock()
    rl, exists := shard.resourceLocks[key]
    shard.mu.Unlock()
    
    if !exists {
        return false
    }
    
    // 2. 获取资源锁
    rl.mu.Lock()
    defer rl.mu.Unlock()
    
    // 3. 检查是否是锁的持有者
    if rl.lockInfo == nil || rl.lockInfo.Request.NodeID != request.NodeID {
        return false
    }
    
    // 4. 更新操作状态
    rl.lockInfo.Completed = true
    rl.lockInfo.Success = (request.Error == "")
    rl.lockInfo.CompletedAt = time.Now()
    
    if rl.lockInfo.Success {
        // 操作成功：立即释放资源锁，通过 SSE 通知所有订阅者
        log.Printf("[Unlock] 操作成功: key=%s, node=%s", key, request.NodeID)
        
        // 广播成功事件（在释放锁之前，确保订阅者能收到事件）
        lm.broadcastEvent(rl, &OperationEvent{
            Type:        request.Type,
            ResourceID:  request.ResourceID,
            NodeID:      request.NodeID,
            Success:     true,
            Error:       request.Error,
            CompletedAt: rl.lockInfo.CompletedAt,
        })
        
        // 立即释放资源锁（删除 lockInfo）
        // 等待的节点通过 SSE 事件已经知道操作已完成，不需要保留锁
        rl.lockInfo = nil
        
        // 注意：不分配锁给等待队列，因为操作成功，资源已存在
        // 等待队列中的节点通过 SSE 收到事件后，会跳过操作
        
    } else {
        // 操作失败：从对应操作类型的等待队列中取出下一个节点
        log.Printf("[Unlock] 操作失败，唤醒队列: key=%s, node=%s", key, request.NodeID)
        
        nextRequest := lm.getNextFromQueue(rl, request.Type)
        if nextRequest != nil {
            // 分配锁给下一个节点
            rl.lockInfo = &LockInfo{
                Request:    nextRequest,
                AcquiredAt: time.Now(),
                Completed:  false,
                Success:    false,
            }
            // 通知该节点
            lm.notifyLockAssigned(rl, nextRequest.NodeID)
        } else {
            // 没有等待队列，清理锁
            rl.lockInfo = nil
        }
    }
    
    return true
}
```

##### 队列管理

```go
// 添加请求到对应操作类型的等待队列
func (lm *LockManager) addToQueue(rl *resourceLock, operationType string, request *LockRequest) {
    if rl.queues == nil {
        rl.queues = make(map[string][]*LockRequest)
    }
    if _, exists := rl.queues[operationType]; !exists {
        rl.queues[operationType] = make([]*LockRequest, 0)
    }
    rl.queues[operationType] = append(rl.queues[operationType], request)
}

// 从对应操作类型的等待队列中取出下一个节点
func (lm *LockManager) getNextFromQueue(rl *resourceLock, operationType string) *LockRequest {
    if rl.queues == nil {
        return nil
    }
    queue, exists := rl.queues[operationType]
    if !exists || len(queue) == 0 {
        return nil
    }
    
    // FIFO：取出第一个
    nextRequest := queue[0]
    rl.queues[operationType] = queue[1:]
    
    // 如果队列为空，删除
    if len(rl.queues[operationType]) == 0 {
        delete(rl.queues, operationType)
    }
    
    return nextRequest
}
```

### 方案B：操作状态独立存储

#### 设计思路

将操作状态与资源锁分离，使用独立的状态存储。

#### 数据结构

```go
type resourceLock struct {
    mu sync.Mutex  // 资源锁（真实锁）
    
    // 当前锁的持有者
    lockInfo *LockInfo
    
    // 等待队列：按操作类型分组
    queues map[string][]*LockRequest
    
    // 订阅者
    subscribers []Subscriber
}

// 操作状态独立存储
type operationStatus struct {
    mu sync.RWMutex
    statuses map[string]*OperationStatus  // key: resourceKey, value: 操作状态
}

type OperationStatus struct {
    Completed bool
    Success   bool
    CompletedAt time.Time
}
```

#### 优缺点

**优点**：
- 操作状态与资源锁分离，逻辑更清晰
- 操作完成后可以保留状态，不影响资源锁

**缺点**：
- 需要额外的状态存储
- 需要管理状态的生命周期

## 关键问题分析

### 问题1：操作成功后，如何让后续节点知道应该跳过？

**⚠️ 重要澄清**：操作成功后应该**立即释放锁**，而不是等到下一个 TryLock 请求时才释放！

#### 当前实现（正确的方式）

```go
// Unlock 时操作成功
if rl.lockInfo.Success {
    // 1. 立即广播 SSE 事件（在释放锁之前）
    lm.broadcastEvent(rl, &OperationEvent{Success: true})
    
    // 2. 立即释放资源锁（删除 lockInfo）
    rl.lockInfo = nil
    
    // 3. 不分配锁给等待队列
    // 等待的节点通过 SSE 事件已经知道操作已完成
}
```

**关键点**：
- ✅ **操作成功时立即释放锁**，不等待后续请求
- ✅ **通过 SSE 事件通知**等待的节点
- ✅ **等待的节点收到 SSE 事件后，跳过操作**

#### 方案对比

##### 方案A：立即释放锁 + SSE 事件通知（当前实现，推荐）

```go
// Unlock 时
if success {
    broadcastEvent()  // 1. 广播事件
    rl.lockInfo = nil  // 2. 立即释放锁
}
```

**优点**：
- ✅ 锁立即释放，不会一直占用
- ✅ 等待的节点通过 SSE 实时收到通知
- ✅ 不依赖后续请求，锁会自动释放

**缺点**：
- ⚠️ 如果 SSE 连接失败，节点可能无法及时收到通知
- ⚠️ 后续节点（没有订阅的）无法通过 TryLock 发现操作已完成

##### 方案B：保留状态 + TryLock 时检查（不推荐）

```go
// Unlock 时
if success {
    broadcastEvent()
    // 保留 lockInfo，不释放锁
}

// TryLock 时
if rl.lockInfo != nil && rl.lockInfo.Completed && rl.lockInfo.Success {
    return false, true, ""  // skip=true
}
```

**问题**：
- ❌ **如果后续没有节点发送 TryLock 请求，锁就一直不释放**
- ❌ **需要额外的状态清理机制**（定时器或手动清理）
- ❌ **内存泄漏风险**：如果忘记清理，状态会一直保留

##### 方案C：混合方案（推荐）

结合方案A和方案B的优点：

```go
// Unlock 时
if success {
    // 1. 立即广播 SSE 事件
    broadcastEvent()
    
    // 2. 立即释放资源锁（删除 lockInfo）
    rl.lockInfo = nil
    
    // 3. 可选：保留操作状态到独立的状态存储（有时间限制）
    // 这样后续 TryLock 也能发现操作已完成
    lm.saveOperationStatus(key, &OperationStatus{
        Completed: true,
        Success:   true,
        CompletedAt: time.Now(),
    })
}

// TryLock 时
// 1. 先检查独立的状态存储
if status := lm.getOperationStatus(key); status != nil && status.Success {
    return false, true, ""  // skip=true
}

// 2. 再检查资源锁
if rl.lockInfo != nil {
    // ...
}
```

**优点**：
- ✅ 锁立即释放，不会一直占用
- ✅ 等待的节点通过 SSE 实时收到通知
- ✅ 后续节点（没有订阅的）也能通过 TryLock 发现操作已完成
- ✅ 状态有自动过期机制，不会内存泄漏

**缺点**：
- ⚠️ 需要额外的状态存储
- ⚠️ 需要管理状态的生命周期

#### 推荐方案：方案A（当前实现）

**理由**：
1. ✅ **锁立即释放**，不会一直占用
2. ✅ **SSE 事件通知**已经足够实时
3. ✅ **实现简单**，不需要额外的状态管理
4. ✅ **等待的节点都会建立 SSE 连接**，能及时收到通知

**对于没有订阅的后续节点**：
- 如果资源已存在，客户端在请求锁之前会先检查（`ShouldSkipOperation`）
- 如果资源不存在，可以重新请求锁（此时锁已释放，可以重新获取）

### 问题2：如何区分不同操作类型的等待队列？

#### 实现方式

```go
type resourceLock struct {
    mu sync.Mutex
    
    // 等待队列：按操作类型分组
    queues map[string][]*LockRequest  // key: "pull" | "update" | "delete"
    
    // ...
}

// 添加请求到对应操作类型的队列
func addToQueue(rl *resourceLock, operationType string, request *LockRequest) {
    if rl.queues == nil {
        rl.queues = make(map[string][]*LockRequest)
    }
    if _, exists := rl.queues[operationType]; !exists {
        rl.queues[operationType] = make([]*LockRequest, 0)
    }
    rl.queues[operationType] = append(rl.queues[operationType], request)
}

// 从对应操作类型的队列中取出下一个节点
func getNextFromQueue(rl *resourceLock, operationType string) *LockRequest {
    queue, exists := rl.queues[operationType]
    if !exists || len(queue) == 0 {
        return nil
    }
    nextRequest := queue[0]
    rl.queues[operationType] = queue[1:]
    return nextRequest
}
```

#### 使用场景

**场景1：Pull 操作失败**
```
节点A pull 失败 → 从 queues["pull"] 中取出节点B → 分配锁给节点B
```

**场景2：Update 操作失败**
```
节点A update 失败 → 从 queues["update"] 中取出节点B → 分配锁给节点B
```

**场景3：Delete 操作失败**
```
节点A delete 失败 → 从 queues["delete"] 中取出节点B → 分配锁给节点B
```

### 问题3：操作状态的生命周期管理

#### 方案1：立即清理（不推荐）

```go
// 操作成功后立即清理
if rl.lockInfo.Success {
    rl.lockInfo = nil
}
```

**问题**：
- 后续节点无法通过 TryLock 发现操作已完成
- 只能依赖 SSE 事件通知

#### 方案2：延迟清理（推荐）

```go
// 操作成功后保留状态一段时间
if rl.lockInfo.Success {
    // 保留状态，设置清理定时器
    go func() {
        time.Sleep(5 * time.Minute)  // 保留5分钟
        rl.mu.Lock()
        if rl.lockInfo != nil && rl.lockInfo.Completed && rl.lockInfo.Success {
            rl.lockInfo = nil
        }
        rl.mu.Unlock()
    }()
}
```

**优点**：
- 后续节点可以通过 TryLock 发现操作已完成
- 不需要立即清理

**缺点**：
- 需要管理定时器
- 内存占用增加

#### 方案3：下次 TryLock 时清理（推荐）

```go
// TryLock 时检查并清理
if rl.lockInfo != nil && rl.lockInfo.Completed && rl.lockInfo.Success {
    // 检查是否过期（例如：超过1小时）
    if time.Since(rl.lockInfo.CompletedAt) > 1*time.Hour {
        rl.lockInfo = nil
        // 继续处理，创建新锁
    } else {
        // 返回 skip=true
        return false, true, ""
    }
}
```

**优点**：
- 自动清理，无需定时器
- 状态保留时间可控

**缺点**：
- 需要每次 TryLock 时检查

## 完整实现示例

### 数据结构

```go
type resourceLock struct {
    mu sync.Mutex  // 资源锁（真实锁）
    
    // 锁信息（包含操作状态）
    lockInfo *LockInfo
    
    // 等待队列：按操作类型分组
    queues map[string][]*LockRequest  // key: operationType
    
    // 订阅者
    subscribers []Subscriber
}

type LockInfo struct {
    Request     *LockRequest
    AcquiredAt  time.Time
    Completed   bool
    Success     bool
    CompletedAt time.Time
}
```

### TryLock 实现

```go
func (lm *LockManager) TryLock(request *LockRequest) (bool, bool, string) {
    key := LockKey(request.Type, request.ResourceID)
    shard := lm.getShard(key)
    
    // 1. 获取分段锁
    shard.mu.Lock()
    rl := shard.getOrCreateResourceLock(key)
    shard.mu.Unlock()
    
    // 2. 获取资源锁
    rl.mu.Lock()
    defer rl.mu.Unlock()
    
    // 3. 检查操作状态
    if rl.lockInfo != nil {
        if rl.lockInfo.Completed {
            // 检查是否过期（超过1小时）
            if time.Since(rl.lockInfo.CompletedAt) > 1*time.Hour {
                rl.lockInfo = nil
                // 继续处理，创建新锁
            } else if rl.lockInfo.Success {
                // 操作已完成且成功 → 返回 skip=true
                return false, true, ""  // acquired=false, skip=true
            } else {
                // 操作已完成但失败 → 处理对应操作类型的队列
                nextRequest := lm.getNextFromQueue(rl, request.Type)
                if nextRequest != nil {
                    rl.lockInfo = &LockInfo{
                        Request:    nextRequest,
                        AcquiredAt: time.Now(),
                        Completed:  false,
                        Success:    false,
                    }
                    lm.notifyLockAssigned(rl, nextRequest.NodeID)
                    if nextRequest.NodeID == request.NodeID {
                        return true, false, ""
                    }
                } else {
                    rl.lockInfo = nil
                }
            }
        } else {
            // 操作未完成
            if rl.lockInfo.Request.NodeID == request.NodeID {
                // 同一节点重新请求
                rl.lockInfo.Request = request
                rl.lockInfo.AcquiredAt = time.Now()
                return true, false, ""
            }
            // 其他节点持有锁，加入对应操作类型的等待队列
            lm.addToQueue(rl, request.Type, request)
            return false, false, ""
        }
    }
    
    // 4. 资源锁不存在，创建新的资源锁
    rl.lockInfo = &LockInfo{
        Request:    request,
        AcquiredAt: time.Now(),
        Completed:  false,
        Success:    false,
    }
    
    return true, false, ""
}
```

### Unlock 实现

```go
func (lm *LockManager) Unlock(request *UnlockRequest) bool {
    key := LockKey(request.Type, request.ResourceID)
    shard := lm.getShard(key)
    
    shard.mu.Lock()
    rl, exists := shard.resourceLocks[key]
    shard.mu.Unlock()
    
    if !exists {
        return false
    }
    
    rl.mu.Lock()
    defer rl.mu.Unlock()
    
    if rl.lockInfo == nil || rl.lockInfo.Request.NodeID != request.NodeID {
        return false
    }
    
    // 更新操作状态
    rl.lockInfo.Completed = true
    rl.lockInfo.Success = (request.Error == "")
    rl.lockInfo.CompletedAt = time.Now()
    
    if rl.lockInfo.Success {
        // 操作成功：不分配锁给等待队列，通过 SSE 通知
        lm.broadcastEvent(rl, &OperationEvent{
            Type:        request.Type,
            ResourceID:  request.ResourceID,
            NodeID:      request.NodeID,
            Success:     true,
            Error:       request.Error,
            CompletedAt: rl.lockInfo.CompletedAt,
        })
        // 注意：保留 lockInfo，让后续 TryLock 能够发现操作已完成
    } else {
        // 操作失败：从对应操作类型的等待队列中取出下一个节点
        nextRequest := lm.getNextFromQueue(rl, request.Type)
        if nextRequest != nil {
            rl.lockInfo = &LockInfo{
                Request:    nextRequest,
                AcquiredAt: time.Now(),
                Completed:  false,
                Success:    false,
            }
            lm.notifyLockAssigned(rl, nextRequest.NodeID)
        } else {
            rl.lockInfo = nil
        }
    }
    
    return true
}
```

## 总结

### ✅ 可行性结论

**使用物理锁（分段锁 + 资源锁）完全可以实现"操作成功后跳过"机制！**

### 关键设计点

1. **保留操作状态**：
   - 即使使用物理锁，也需要保留操作状态（Completed、Success）
   - 用于判断操作是否已完成、是否成功

2. **操作类型分组的等待队列**：
   - 使用 `map[string][]*LockRequest` 按操作类型分组
   - 操作失败时，从对应操作类型的队列中取出下一个节点

3. **操作成功后的处理**：
   - 不分配锁给等待队列
   - 通过 SSE 事件通知所有订阅者
   - 保留操作状态一段时间，让后续 TryLock 能够发现操作已完成

4. **操作失败后的处理**：
   - 从对应操作类型的等待队列中取出下一个节点
   - 分配锁给该节点
   - 通过 SSE 通知该节点

### 实现建议

1. **操作状态保留时间**：建议保留1小时，或根据实际场景调整
2. **状态清理**：在 TryLock 时检查并清理过期状态
3. **队列管理**：按操作类型分组，确保不同操作类型有独立的等待队列
4. **事件通知**：结合 TryLock 状态检查和 SSE 事件通知，确保可靠性

