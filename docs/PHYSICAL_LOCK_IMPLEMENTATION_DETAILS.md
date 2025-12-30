# 物理锁实现细节确认

## 当前实现 vs 目标实现

### 当前实现
- **分段锁**：`shard.mu sync.RWMutex`（物理锁）✅
- **资源锁**：`shard.locks map[string]*LockInfo`（逻辑锁，只是状态标记）❌

### 目标实现
- **分段锁**：`shard.mu sync.RWMutex`（物理锁）✅
- **资源锁**：`resourceLocks map[string]*sync.Mutex`（物理锁）✅

---

## 需要确认的实现细节

### 1. 资源锁的存储结构

#### 问题1.1：如何存储资源锁？

**方案A：独立存储**
```go
type resourceShard struct {
    mu sync.RWMutex  // 分段锁
    
    // 资源锁：key -> Mutex
    resourceLocks map[string]*sync.Mutex
    
    // 锁信息：key -> LockInfo
    locks map[string]*LockInfo
    
    queues map[string][]*LockRequest
    subscribers map[string][]*Subscriber
}
```

**方案B：嵌入到LockInfo**
```go
type LockInfo struct {
    mu sync.Mutex  // 资源锁（物理锁）
    Request *LockRequest
    AcquiredAt time.Time
    Completed bool
    Success bool
    CompletedAt time.Time
}
```

**推荐：方案A**
- ✅ 锁和状态分离，逻辑更清晰
- ✅ 锁的生命周期独立管理
- ✅ 更容易处理锁的创建和销毁

#### 问题1.2：资源锁的初始化时机？

**选项**：
1. **延迟初始化**：第一次TryLock时创建
2. **预初始化**：提前创建所有可能的资源锁（不推荐，内存浪费）

**推荐：延迟初始化**
- ✅ 节省内存
- ✅ 只创建实际使用的资源锁

---

### 2. 锁的获取顺序（关键：防止死锁）

#### 问题2.1：分段锁和资源锁的获取顺序？

**⚠️ 关键问题**：访问 `shard.locks[key]` 需要分段锁保护，但获取资源锁后不能持有分段锁（避免死锁）

**解决方案：分阶段获取锁**

```go
func (lm *LockManager) TryLock(request *LockRequest) (bool, bool, string) {
    key := LockKey(request.Type, request.ResourceID)
    shard := lm.getShard(request.ResourceID)
    
    // 阶段1：获取分段锁，获取或创建资源锁
    shard.mu.Lock()
    resourceLock := lm.getOrCreateResourceLock(shard, key)
    shard.mu.Unlock()  // 立即释放分段锁
    
    // 阶段2：获取资源锁
    resourceLock.Lock()
    defer resourceLock.Unlock()
    
    // 阶段3：重新获取分段锁（读锁）来访问 locks map
    shard.mu.RLock()
    lockInfo, exists := shard.locks[key]
    shard.mu.RUnlock()
    
    // 阶段4：根据检查结果处理
    if exists {
        // 锁已存在，处理逻辑...
    } else {
        // 锁不存在，创建新锁（需要写锁）
        shard.mu.Lock()
        shard.locks[key] = &LockInfo{...}
        shard.mu.Unlock()
    }
}
```

**⚠️ 关键点**：
- ✅ **分段锁保护 map 访问**（resourceLocks, locks, queues）
- ✅ **资源锁保护业务逻辑的互斥**（确保同一资源同一时刻只有一个操作）
- ✅ **不能同时持有分段锁和资源锁**（会导致死锁）
- ⚠️ **需要分阶段获取锁**（分段锁 → 资源锁 → 分段锁）

#### 问题2.2：为什么不能同时持有两个锁？

**死锁场景**：
```
节点A：持有分段锁 → 尝试获取资源锁1
节点B：持有分段锁 → 尝试获取资源锁2
节点A：需要资源锁2（被B持有）
节点B：需要资源锁1（被A持有）
→ 死锁！
```

**解决方案**：
- 分段锁只用于保护 `resourceLocks` map 的访问
- 获取资源锁后立即释放分段锁
- 资源锁用于保护实际的业务逻辑

---

### 3. 锁的释放顺序

#### 问题3.1：何时释放资源锁？

**选项**：
1. **操作完成后立即释放**（推荐）
2. **保留一段时间再释放**（不推荐，可能导致内存泄漏）

**推荐：操作完成后立即释放**

```go
func (lm *LockManager) Unlock(request *UnlockRequest) bool {
    key := LockKey(request.Type, request.ResourceID)
    shard := lm.getShard(request.ResourceID)
    
    // 1. 获取分段锁（用于访问resourceLocks map）
    shard.mu.Lock()
    resourceLock, exists := shard.resourceLocks[key]
    shard.mu.Unlock()
    
    if !exists {
        return false
    }
    
    // 2. 获取资源锁
    resourceLock.Lock()
    defer resourceLock.Unlock()
    
    // 3. 检查锁状态
    lockInfo, exists := shard.locks[key]
    if !exists {
        return false
    }
    
    // 4. 更新状态
    lockInfo.Completed = true
    lockInfo.Success = (request.Error == "")
    
    // 5. 操作成功：删除锁和资源锁
    if lockInfo.Success {
        delete(shard.locks, key)
        // 删除资源锁（需要重新获取分段锁）
        shard.mu.Lock()
        delete(shard.resourceLocks, key)
        shard.mu.Unlock()
    } else {
        // 操作失败：保留锁，分配给队列中的下一个节点
        // ...
    }
    
    return true
}
```

---

### 4. 锁的生命周期管理

#### 问题4.1：何时创建资源锁？

**时机**：第一次TryLock时

```go
func (lm *LockManager) getOrCreateResourceLock(shard *resourceShard, key string) *sync.Mutex {
    // 分段锁必须已经获取
    if lock, exists := shard.resourceLocks[key]; exists {
        return lock
    }
    
    // 创建新的资源锁
    lock := &sync.Mutex{}
    shard.resourceLocks[key] = lock
    return lock
}
```

#### 问题4.2：何时销毁资源锁？

**时机**：
1. **操作成功时**：立即删除（因为后续节点不需要锁）
2. **操作失败时**：保留锁（分配给队列中的下一个节点）

**⚠️ 注意**：
- 删除资源锁时，必须确保没有其他goroutine正在使用它
- 删除前需要重新获取分段锁

---

### 5. "Skip if Successful"机制

#### 问题5.1：操作成功后，如何让后续节点跳过？

**当前机制**（逻辑锁）：
- 操作成功时，删除 `shard.locks[key]`
- 后续节点TryLock时，发现锁不存在，可以获取锁
- 但客户端会先检查引用计数，如果资源已存在，不会请求锁

**物理锁机制**：
- 操作成功时，删除 `shard.locks[key]` 和 `shard.resourceLocks[key]`
- 后续节点TryLock时，需要创建新的资源锁
- **但客户端会先检查引用计数，如果资源已存在，不会请求锁**

**✅ 结论**：机制保持不变，客户端检查引用计数是关键

#### 问题5.2：操作成功后，是否需要通知等待队列？

**当前实现**：
- 操作成功时，不调用 `processQueue`
- 通过SSE通知所有订阅者
- 订阅者收到事件后，重新检查资源状态

**物理锁实现**：
- **保持不变**
- 操作成功时，删除锁和资源锁
- 通过SSE通知所有订阅者
- 订阅者收到事件后，重新检查资源状态

---

### 6. 操作失败后的处理

#### 问题6.1：操作失败后，如何通知队列中的下一个节点？

**当前机制**（逻辑锁）：
1. 操作失败时，删除 `shard.locks[key]`
2. 调用 `processQueue` 分配锁给队列中的下一个节点
3. 通过 `notifyLockAssigned` 通知队头节点

**物理锁机制**：
- **保持不变**
- 操作失败时，保留资源锁（不删除）
- 调用 `processQueue` 分配锁给队列中的下一个节点
- 通过 `notifyLockAssigned` 通知队头节点

**⚠️ 注意**：
- 资源锁必须保留，因为下一个节点需要使用它
- 只有操作成功时才删除资源锁

---

### 7. 并发安全性

#### 问题7.1：如何确保资源锁的并发安全？

**关键点**：
1. **分段锁保护 `resourceLocks` map 的访问**
2. **资源锁保护 `locks[key]` 的访问**
3. **不能同时持有两个锁**

**正确的获取顺序**：
```go
// 1. 获取分段锁
shard.mu.Lock()

// 2. 获取或创建资源锁
resourceLock := getOrCreateResourceLock(shard, key)

// 3. 释放分段锁（关键！）
shard.mu.Unlock()

// 4. 获取资源锁
resourceLock.Lock()
defer resourceLock.Unlock()

// 5. 访问 locks[key]
```

#### 问题7.2：如何避免竞态条件？

**场景**：两个节点同时TryLock同一个资源

**流程**：
```
节点A：获取分段锁 → 创建资源锁 → 释放分段锁 → 获取资源锁 → 创建LockInfo
节点B：获取分段锁 → 发现资源锁已存在 → 释放分段锁 → 等待资源锁 → 发现LockInfo已存在 → 加入队列
```

**✅ 正确**：资源锁确保同一时刻只有一个节点能创建LockInfo

---

### 8. 内存管理
#### 问题8.1：如何避免资源锁泄漏？

**风险**：
- 如果操作失败后，节点崩溃，资源锁可能永远不会被删除

**解决方案**：
1. **操作成功时立即删除**（正常流程）
2. **操作失败时保留锁**（分配给下一个节点）
3. **添加超时机制**（可选，如果节点长时间不响应，清理锁）

**推荐**：
- 先实现基本功能
- 后续添加超时清理机制（如果发现内存泄漏）

#### 问题8.2：资源锁的内存占用？

**估算**：
- 每个 `sync.Mutex` 约 8 字节
- 1000个资源锁 ≈ 8KB（可忽略）

**结论**：内存占用很小，可以接受

---

### 9. 代码结构变更

#### 问题9.1：需要修改哪些函数？

**需要修改的函数**：
1. `TryLock` - 添加资源锁的获取逻辑
2. `Unlock` - 添加资源锁的释放逻辑
3. `processQueue` - 确保资源锁已存在
4. `NewLockManager` - 初始化 `resourceLocks` map

**新增函数**：
1. `getOrCreateResourceLock` - 获取或创建资源锁

#### 问题9.2：数据结构变更？

**变更**：
```go
type resourceShard struct {
    mu sync.RWMutex
    
    // 新增：资源锁（物理锁）
    resourceLocks map[string]*sync.Mutex
    
    // 保持不变
    locks map[string]*LockInfo
    queues map[string][]*LockRequest
    subscribers map[string][]Subscriber
}
```

---

### 10. 测试场景

#### 问题10.1：需要测试哪些场景？

1. **基本功能**：
   - ✅ 单个节点获取锁成功
   - ✅ 多个节点竞争同一个资源锁
   - ✅ 操作成功后，后续节点跳过

2. **并发场景**：
   - ✅ 多个节点同时TryLock不同资源（不同分段）
   - ✅ 多个节点同时TryLock同一资源（同一分段）
   - ✅ 操作失败后，队列中的节点获取锁

3. **边界情况**：
   - ✅ 资源锁的创建和销毁
   - ✅ 死锁预防
   - ✅ 内存泄漏检查

---

## 实现方案总结

### 核心原则

1. **锁的获取顺序**：分段锁 → 资源锁（不能同时持有）
2. **锁的释放顺序**：先释放分段锁，再获取资源锁
3. **资源锁的生命周期**：
   - 创建：第一次TryLock时
   - 销毁：操作成功时
   - 保留：操作失败时（分配给下一个节点）

### 关键代码结构

```go
type resourceShard struct {
    mu sync.RWMutex  // 分段锁（物理锁）
    
    resourceLocks map[string]*sync.Mutex  // 资源锁（物理锁）
    locks map[string]*LockInfo  // 锁状态
    queues map[string][]*LockRequest
    subscribers map[string][]Subscriber
}

func (lm *LockManager) TryLock(request *LockRequest) (bool, bool, string) {
    // 1. 获取分段锁
    shard.mu.Lock()
    
    // 2. 获取或创建资源锁
    resourceLock := lm.getOrCreateResourceLock(shard, key)
    
    // 3. 释放分段锁（关键！）
    shard.mu.Unlock()
    
    // 4. 获取资源锁
    resourceLock.Lock()
    defer resourceLock.Unlock()
    
    // 5. 检查锁状态（需要重新获取分段锁来访问locks map）
    shard.mu.RLock()
    lockInfo, exists := shard.locks[key]
    shard.mu.RUnlock()
    
    // 6. 处理逻辑...
}
```

---

## 待确认的问题

### 问题1：分段锁和资源锁的获取顺序

**确认**：是否同意"分阶段获取锁"的方案？
- 阶段1：获取分段锁 → 获取资源锁 → 释放分段锁
- 阶段2：获取资源锁
- 阶段3：重新获取分段锁（读锁）访问 locks map
- 阶段4：根据结果决定是否需要写锁更新 locks map

**⚠️ 注意**：这个方案会增加锁的获取次数，可能影响性能

### 问题2：资源锁的生命周期

**确认**：
- 创建时机：第一次TryLock时 ✅
- 销毁时机：操作成功时 ✅
- 保留时机：操作失败时 ✅

### 问题3：操作成功后的处理

**确认**：是否保持当前机制（删除锁，通过SSE通知，客户端检查引用计数）？

### 问题4：操作失败后的处理

**确认**：是否保持当前机制（保留锁，分配给队列中的下一个节点）？

### 问题5：内存管理

**确认**：是否需要添加超时清理机制（防止资源锁泄漏）？

### 问题6：物理锁的实际作用

**确认**：物理锁的实际作用是什么？
- **选项A**：保护业务操作的互斥（但业务操作在锁外执行，所以这个锁实际上没什么用）
- **选项B**：作为状态标记，确保同一资源同一时刻只有一个操作在进行（类似逻辑锁）
- **选项C**：提供更细粒度的锁控制，减少锁竞争（但会增加锁获取次数）

**⚠️ 需要明确**：如果业务操作在锁外执行，物理锁和逻辑锁的区别是什么？

---

## 下一步行动

1. **确认以上问题**
2. **实现代码变更**
3. **编写测试用例**
4. **性能测试**（对比逻辑锁和物理锁的性能）

