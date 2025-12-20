# 两层判断机制详解

## 概述

系统使用**两层判断机制**来决定是否需要执行操作：
1. **客户端本地判断**：在请求锁之前，检查本地引用计数
2. **服务端状态判断**：在等待锁的过程中，查询服务器端锁的状态

---

## 1. 客户端本地判断（Client Local Check）

### 定义

**客户端本地判断**是指在**请求分布式锁之前**，客户端先检查自己本地的引用计数，判断资源是否已经存在。

### 实现位置

#### 1.1 入口：`OpenWriter` 函数

**文件**：`conchContent-v3/lockintegration/writer.go`  
**行数**：第 56-65 行

```go
// 在获取锁之前，先用本地计数判断是否应执行操作
skip, errMsg := writer.refCountManager.ShouldSkipOperation(lockcallback.OperationTypePull, writer.resourceID)
if skip {
    writer.skipped = true
    writer.locked = false
    return writer, nil  // 直接返回，不请求锁
}
if errMsg != "" {
    return nil, fmt.Errorf("操作被拒绝: %s", errMsg)
}
```

**作用**：
- 在请求锁之前，先调用 `ShouldSkipOperation` 检查本地引用计数
- 如果 `skip = true`，说明资源已存在，直接跳过操作，**不请求锁**
- 如果 `skip = false`，继续执行，请求分布式锁

#### 1.2 核心逻辑：`ShouldSkipOperation` 函数

**文件**：`conchContent-v3/lockcallback/manager.go`  
**行数**：第 72-100 行

```go
func (m *RefCountManager) ShouldSkipOperation(operationType, resourceID string) (bool, string) {
    refCount := m.GetRefCount(resourceID)  // 从本地存储读取引用计数

    switch operationType {
    case OperationTypePull:
        // Pull逻辑：如果refcount != 0，说明已经下载完成，应该跳过
        if refCount.Count > 0 {
            return true, ""  // 跳过操作
        }
    
    case OperationTypeDelete:
        // Delete逻辑：如果refcount > 0，不能执行delete操作
        if refCount.Count > 0 {
            return false, "无法删除：当前有节点正在使用该资源"
        }
    
    case OperationTypeUpdate:
        // Update操作不基于refcount来决定是否跳过
    }

    return false, ""  // 不跳过，需要执行操作
}
```

**作用**：
- 从本地存储（内存/文件/数据库）读取引用计数
- 根据操作类型和引用计数判断是否应该跳过操作
- **Pull 操作**：如果 `refCount.Count > 0`，说明资源已存在，跳过
- **Delete 操作**：如果 `refCount.Count > 0`，说明有节点在使用，拒绝删除

#### 1.3 本地存储：`LocalRefCountStorage`

**文件**：`conchContent-v3/lockintegration/refcount_storage.go`  
**行数**：第 14-73 行

```go
type LocalRefCountStorage struct {
    mu        sync.RWMutex
    refCounts map[string]*lockcallback.ReferenceCount  // 内存中的引用计数
}

func (s *LocalRefCountStorage) GetRefCount(resourceID string) *lockcallback.ReferenceCount {
    s.mu.RLock()
    ref, ok := s.refCounts[resourceID]
    s.mu.RUnlock()
    if !ok {
        return &lockcallback.ReferenceCount{
            Count: 0,
            Nodes: map[string]bool{},
        }
    }
    // 返回副本
    return &lockcallback.ReferenceCount{
        Count: ref.Count,
        Nodes: ref.Nodes,
    }
}
```

**作用**：
- 在客户端本地（内存中）存储引用计数
- 每个节点维护自己的引用计数副本
- 通过 `GetRefCount` 读取引用计数

### 工作流程

```
节点B想要下载资源
  ↓
调用 OpenWriter()
  ↓
调用 ShouldSkipOperation() 检查本地引用计数
  ↓
从 LocalRefCountStorage 读取 refCount
  ↓
如果 refCount.Count > 0 → skip = true → 跳过操作，不请求锁 ✅
如果 refCount.Count == 0 → skip = false → 继续请求锁
```

### 优点

1. **避免不必要的网络请求**：如果资源已存在，直接跳过，不请求锁
2. **快速响应**：本地内存查询，速度快
3. **减少服务器压力**：减少锁请求的数量

### 缺点

1. **可能不准确**：本地引用计数可能没有及时更新（mergerfs同步延迟）
2. **需要同步**：多个节点之间的引用计数需要同步

---

## 2. 服务端状态判断（Server Status Check）

### 定义

**服务端状态判断**是指在**等待锁的过程中**，客户端通过轮询服务器端，查询锁的状态，判断操作是否已经完成。

### 实现位置

#### 2.1 客户端轮询：`waitForLock` 函数

**文件**：`conchContent-v3/lockclient/client.go`  
**行数**：第 151-209 行

```go
func (c *LockClient) waitForLock(ctx context.Context, request *Request) (*LockResult, error) {
    ticker := time.NewTicker(500 * time.Millisecond) // 每500ms轮询一次
    defer ticker.Stop()

    for {
        select {
        case <-ctx.Done():
            return nil, ctx.Err()
        case <-ticker.C:
            // 查询锁状态
            req, err := http.NewRequestWithContext(ctx, "GET", c.ServerURL+"/lock/status", bytes.NewBuffer(jsonData))
            // ...
            
            var statusResp struct {
                Acquired  bool   `json:"acquired"`
                Completed bool   `json:"completed"` // 操作是否完成
                Success   bool   `json:"success"`   // 操作是否成功
                Error     string `json:"error"`
            }
            // ...
            
            // 如果操作已完成且成功，说明其他节点已经完成，当前节点跳过操作
            if statusResp.Completed && statusResp.Success {
                return &LockResult{
                    Acquired: false,
                    Skipped:  true,  // 跳过操作
                }, nil
            }
        }
    }
}
```

**作用**：
- 当客户端请求锁但未获得时（`acquired = false`），进入 `waitForLock` 轮询
- 每 500ms 查询一次 `/lock/status` 接口
- 如果发现 `Completed = true && Success = true`，说明其他节点已完成操作
- 返回 `Skipped: true`，跳过操作

#### 2.2 服务器端接口：`LockStatus` 处理函数

**文件**：`server/handler.go`  
**行数**：第 93-117 行

```go
func (h *Handler) LockStatus(w http.ResponseWriter, r *http.Request) {
    var request LockRequest
    if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
        http.Error(w, "无效的请求格式", http.StatusBadRequest)
        return
    }

    // 获取锁状态
    acquired, completed, success := h.lockManager.GetLockStatus(request.Type, request.ResourceID, request.NodeID)

    response := map[string]interface{}{
        "acquired":  acquired,   // 是否是当前节点持有的锁
        "completed": completed,  // 操作是否完成
        "success":   success,    // 操作是否成功
    }

    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(response)
}
```

**作用**：
- 接收客户端的 `/lock/status` 请求
- 调用 `GetLockStatus` 查询锁的状态
- 返回锁的状态信息

#### 2.3 服务器端核心逻辑：`GetLockStatus` 函数

**文件**：`server/lock_manager.go`  
**行数**：第 138-156 行

```go
func (lm *LockManager) GetLockStatus(lockType, resourceID, nodeID string) (bool, bool, bool) {
    key := LockKey(lockType, resourceID)
    shard := lm.getShard(key)

    shard.mu.RLock()
    defer shard.mu.RUnlock()

    lockInfo, exists := shard.locks[key]

    if !exists {
        return false, false, false // 没有锁，未完成，未成功
    }

    // 检查是否是当前节点持有的锁
    acquired := lockInfo.Request.NodeID == nodeID

    return acquired, lockInfo.Completed, lockInfo.Success
}
```

**作用**：
- 查询指定资源的锁信息
- 返回三个值：
  - `acquired`：是否是当前节点持有的锁
  - `completed`：操作是否完成
  - `success`：操作是否成功

### 工作流程

```
节点B请求锁，但锁被占用（节点A正在下载）
  ↓
返回 acquired = false，进入 waitForLock() 轮询
  ↓
每500ms查询一次 /lock/status
  ↓
服务器返回锁状态：{acquired: false, completed: true, success: true}
  ↓
客户端发现操作已完成且成功 → 返回 Skipped: true
  ↓
更新本地引用计数，跳过操作
```

### 优点

1. **实时性**：可以实时知道其他节点的操作状态
2. **准确性**：服务器端的状态是准确的，不会因为本地同步延迟而错误
3. **处理队列场景**：当节点在队列中等待时，可以及时发现操作已完成

### 缺点

1. **网络开销**：需要轮询服务器，增加网络请求
2. **延迟**：轮询间隔（500ms）可能导致延迟

---

## 3. 两层判断的配合

### 完整流程

```
节点B想要下载资源
  ↓
【第一层：客户端本地判断】
  ↓
调用 ShouldSkipOperation() 检查本地引用计数
  ↓
如果 refCount.Count > 0 → 跳过操作，不请求锁 ✅
如果 refCount.Count == 0 → 继续
  ↓
【请求分布式锁】
  ↓
如果 acquired = true → 获得锁，执行操作
如果 acquired = false → 进入等待
  ↓
【第二层：服务端状态判断】
  ↓
进入 waitForLock() 轮询
  ↓
每500ms查询 /lock/status
  ↓
如果 completed = true && success = true → 跳过操作 ✅
如果 completed = false → 继续等待
```

### 代码调用链

#### 第一层：客户端本地判断

```
OpenWriter()
  ↓
ShouldSkipOperation()  ← conchContent-v3/lockcallback/manager.go:76
  ↓
GetRefCount()  ← conchContent-v3/lockintegration/refcount_storage.go:28
  ↓
读取本地内存中的引用计数
```

#### 第二层：服务端状态判断

```
ClusterLock()
  ↓
Lock()  ← conchContent-v3/lockclient/client.go:40
  ↓
tryLockOnce()  ← conchContent-v3/lockclient/client.go:71
  ↓
如果 acquired = false → waitForLock()  ← conchContent-v3/lockclient/client.go:152
  ↓
查询 /lock/status  ← GET http://server:8080/lock/status
  ↓
LockStatus()  ← server/handler.go:94
  ↓
GetLockStatus()  ← server/lock_manager.go:140
  ↓
返回锁状态：{acquired, completed, success}
```

---

## 4. 关键代码位置总结

### 客户端本地判断

| 功能 | 文件 | 行数 | 说明 |
|------|------|------|------|
| 入口 | `conchContent-v3/lockintegration/writer.go` | 56-65 | `OpenWriter` 中调用 `ShouldSkipOperation` |
| 核心逻辑 | `conchContent-v3/lockcallback/manager.go` | 76-100 | `ShouldSkipOperation` 判断是否跳过 |
| 本地存储 | `conchContent-v3/lockintegration/refcount_storage.go` | 28-48 | `GetRefCount` 读取本地引用计数 |

### 服务端状态判断

| 功能 | 文件 | 行数 | 说明 |
|------|------|------|------|
| 客户端轮询 | `conchContent-v3/lockclient/client.go` | 152-209 | `waitForLock` 轮询查询状态 |
| 服务器接口 | `server/handler.go` | 94-117 | `LockStatus` 处理 `/lock/status` 请求 |
| 服务器逻辑 | `server/lock_manager.go` | 140-156 | `GetLockStatus` 查询锁状态 |

---

## 5. 示例场景

### 场景1：本地判断生效

```
节点A已下载资源，更新了本地引用计数：refCount.Count = 1
  ↓
节点B想要下载同一资源
  ↓
调用 ShouldSkipOperation() → refCount.Count = 1 > 0 → skip = true
  ↓
直接跳过操作，不请求锁 ✅
```

### 场景2：服务端状态判断生效

```
节点A正在下载资源（持有锁）
节点B请求锁 → acquired = false，进入 waitForLock()
  ↓
节点A下载完成，释放锁
  ↓
节点B轮询查询 /lock/status
  ↓
服务器返回：{completed: true, success: true}
  ↓
节点B发现操作已完成 → 跳过操作，更新本地引用计数 ✅
```

---

## 总结

- **客户端本地判断**：在请求锁之前，检查本地引用计数，避免不必要的网络请求
- **服务端状态判断**：在等待锁的过程中，查询服务器状态，及时发现操作已完成

两层判断机制相互配合，既提高了效率（本地判断快速），又保证了准确性（服务端判断可靠）。

