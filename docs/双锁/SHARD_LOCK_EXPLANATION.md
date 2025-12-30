# 分段锁实现机制详解

## ⚠️ 重要更新

**当前实现已修改为：分段只根据 `resourceID`（镜像层的 digest），不包含操作类型。**

**原因**：确保同一镜像层的所有操作类型（pull、update、delete）互斥，避免删除和下载并发执行造成错误。

## 核心问题

1. **分段锁如何保证同一个镜像层的锁会分在同一个分段？**
   - ✅ **答案**：分段只根据 `resourceID`（镜像层的 digest），不包含操作类型
   - ✅ **结果**：同一个镜像层的所有操作类型都会分到同一个分段，保证互斥

2. **分段是根据什么来分的？是 layerid 吗？**
   - ✅ **答案**：是的，分段只根据 `resourceID`（即 layerid/digest）
   - ✅ **不包含操作类型**：操作类型不影响分段，只影响逻辑锁的 key

## 分段锁的实现机制

### 1. 锁的唯一标识（Key）

**位置**：`server/types.go:44-47`

```go
// LockKey 生成锁的唯一标识
func LockKey(lockType, resourceID string) string {
	return lockType + ":" + resourceID
}
```

**说明**：
- `lockType`：操作类型（`"pull"`、`"update"`、`"delete"`）
- `resourceID`：镜像层的 digest（例如：`"sha256:abc123..."`）
- **Key 格式**：`"操作类型:资源ID"`

**示例**：
```go
LockKey("pull", "sha256:abc123...")   // → "pull:sha256:abc123..."
LockKey("update", "sha256:abc123...") // → "update:sha256:abc123..."
LockKey("delete", "sha256:abc123...") // → "delete:sha256:abc123..."
```

### 2. 分段计算逻辑

**位置**：`server/lock_manager.go:37-43`

```go
// getShard 根据resourceID获取对应的分段
// 注意：分段只根据resourceID，不包含操作类型，确保同一镜像层的所有操作类型互斥
func (lm *LockManager) getShard(resourceID string) *resourceShard {
	// 使用FNV-1a哈希算法计算分段索引
	// 只对resourceID进行哈希，确保同一镜像层的所有操作类型（pull、update、delete）分到同一个分段
	h := fnv.New32a()
	h.Write([]byte(resourceID))
	return lm.shards[h.Sum32()%shardCount]
}
```

**说明**：
1. 使用 **FNV-1a 哈希算法**对 `resourceID`（镜像层的 digest）进行哈希
2. 将哈希值对 `shardCount`（32）取模，得到分段索引（0-31）
3. 返回对应的分段
4. **关键**：只根据 `resourceID` 分段，不包含操作类型，确保同一镜像层的所有操作类型互斥

### 3. 完整流程

```go
// TryLock 时
key := LockKey(request.Type, request.ResourceID)  // 1. 生成 key（用于逻辑锁）
shard := lm.getShard(request.ResourceID)          // 2. 根据 resourceID 计算分段（只根据resourceID）
shard.mu.Lock()                                     // 3. 获取分段锁
// ... 操作 ...
shard.mu.Unlock()                                   // 4. 释放分段锁
```

**注意**：
- `key`（`lockType:resourceID`）用于逻辑锁（`shard.locks[key]`），区分不同的操作类型
- `resourceID` 用于分段计算，确保同一镜像层的所有操作类型分到同一个分段

## 关键发现

### ✅ 重要：同一个镜像层的所有操作类型都会分到同一个分段！

**原因**：
- 分段只根据 **`resourceID`**（镜像层的 digest）来分，不包含操作类型
- 同一个镜像层（相同的 `resourceID`）的所有操作类型（pull、update、delete）都会分到同一个分段
- 确保同一镜像层的所有操作类型互斥，避免删除和下载并发执行造成错误

**示例**：
```go
// 同一个镜像层，不同的操作类型
resourceID := "sha256:abc123..."

// 分段计算（只根据 resourceID）
shard1 := getShard("sha256:abc123...")  // → shard[9]
shard2 := getShard("sha256:abc123...")  // → shard[9]（相同）

// 结果：同一个镜像层的所有操作类型都分到同一个分段 ✅
```

### ✅ 逻辑锁仍然区分操作类型

**原因**：
- `LockKey`（`lockType + ":" + resourceID`）用于逻辑锁（`shard.locks[key]`）
- 不同的操作类型使用不同的 `key`，逻辑锁可以区分不同的操作类型
- 但分段锁只根据 `resourceID`，确保同一镜像层的所有操作类型互斥

**示例**：
```go
// 同一个镜像层，不同的操作类型
key1 := LockKey("pull", "sha256:abc123...")   // → "pull:sha256:abc123..."
key2 := LockKey("delete", "sha256:abc123...") // → "delete:sha256:abc123..."

// 分段计算（相同，因为只根据 resourceID）
shard1 := getShard("sha256:abc123...")  // → shard[9]
shard2 := getShard("sha256:abc123...")  // → shard[9]（相同）

// 逻辑锁（不同，因为 key 不同）
lock1 := shard.locks["pull:sha256:abc123..."]    // 逻辑锁1
lock2 := shard.locks["delete:sha256:abc123..."]  // 逻辑锁2

// 结果：
// - 分段锁相同 → 互斥（同一时刻只能有一个操作）
// - 逻辑锁不同 → 可以区分不同的操作类型
```

## 分段规则总结

### 分段依据

**分段只根据 `resourceID`（镜像层的 digest）来分，不包含操作类型！**

### 分段结果

| 场景 | ResourceID | 分段结果 |
|------|------------|---------|
| 同一个镜像层，不同操作类型 | `"sha256:abc"` | ✅ **同一个分段**（互斥） |
| 同一个镜像层，同一操作类型 | `"sha256:abc"` | ✅ **同一个分段**（互斥） |
| 不同镜像层 | `"sha256:abc"` vs `"sha256:def"` | ❌ **可能不同分段**（可以并发） |

### 关键点

1. **同一个镜像层的所有操作类型**：✅ **保证分到同一个分段**（互斥）
2. **不同镜像层**：❌ **可能分到不同分段**（可以并发）
3. **分段只根据 `resourceID`（layerid）**：不包含操作类型
4. **逻辑锁仍然区分操作类型**：`LockKey`（`lockType:resourceID`）用于逻辑锁

## 实际影响分析

### 场景1：同一个镜像层的 Pull 操作

**多个节点同时请求 pull 同一个镜像层**：
```go
节点A: LockKey("pull", "sha256:abc") → shard[9]
节点B: LockKey("pull", "sha256:abc") → shard[9]  // 相同分段
节点C: LockKey("pull", "sha256:abc") → shard[9]  // 相同分段
```

**结果**：
- ✅ 所有请求都分到同一个分段（shard[9]）
- ✅ 通过分段锁保证互斥
- ✅ 通过逻辑锁（`shard.locks[key]`）保证同一时刻只有一个节点能操作

### 场景2：同一个镜像层的不同操作类型

**节点A pull，节点B delete 同一个镜像层**：
```go
节点A: getShard("sha256:abc") → shard[9]
节点B: getShard("sha256:abc") → shard[9]  // 相同分段
```

**结果**：
- ✅ **分到同一个分段**
- ✅ **会竞争同一个分段锁**
- ✅ **互斥执行**（避免删除和下载并发造成错误）

### 场景3：不同镜像层的操作

**节点A pull layer1，节点B pull layer2**：
```go
节点A: LockKey("pull", "sha256:layer1") → shard[5]
节点B: LockKey("pull", "sha256:layer2") → shard[12]  // 可能不同分段
```

**结果**：
- ✅ **分到不同分段**
- ✅ **不会竞争同一个分段锁**
- ✅ **可以并发执行**（这是期望的行为！）

## 已解决的问题

### ✅ 问题：同一个镜像层的不同操作类型可能并发执行（已解决）

**场景**：
- 节点A 正在 pull 镜像层 `sha256:abc`
- 节点B 同时 delete 同一个镜像层 `sha256:abc`

**当前实现**：
- `getShard("sha256:abc")` → shard[9]
- `getShard("sha256:abc")` → shard[9]（相同分段）
- 两个操作会竞争同一个分段锁，**互斥执行** ✅

**解决方案**：
- ✅ **只根据 `resourceID` 分段**（不考虑操作类型）
- ✅ **确保同一镜像层的所有操作类型互斥**
- ✅ **避免删除和下载并发执行造成错误**

## 当前实现（已采用方案1）

### ✅ 只根据 resourceID 分段

**实现**：
```go
// getShard 只根据 resourceID 分段
func (lm *LockManager) getShard(resourceID string) *resourceShard {
	h := fnv.New32a()
	h.Write([]byte(resourceID))  // 只对 resourceID 哈希
	return lm.shards[h.Sum32()%shardCount]
}

// LockKey 仍然包含操作类型（用于逻辑锁）
func LockKey(lockType, resourceID string) string {
	return lockType + ":" + resourceID  // 用于逻辑锁的 key
}
```

**优点**：
- ✅ 同一个镜像层的所有操作类型都会分到同一个分段
- ✅ 保证同一镜像层的所有操作互斥
- ✅ 避免删除和下载并发执行造成错误
- ✅ 逻辑锁仍然区分操作类型（`LockKey` 包含操作类型）

**设计说明**：
- **分段锁**：只根据 `resourceID` 分段，确保同一镜像层的所有操作类型互斥
- **逻辑锁**：使用 `LockKey`（`lockType:resourceID`）区分不同的操作类型

## 总结

### 当前实现

1. **分段依据**：只根据 `resourceID`（镜像层的 digest），不包含操作类型
2. **分段算法**：FNV-1a 哈希 + 取模
3. **保证**：同一个镜像层的所有操作类型（pull、update、delete）都会分到同一个分段
4. **逻辑锁**：使用 `LockKey`（`lockType:resourceID`）区分不同的操作类型

### 关键结论

- ✅ **同一个镜像层的所有操作类型**：保证分到同一个分段（互斥）
- ✅ **不同镜像层**：可能分到不同分段（可以并发）
- ✅ **分段只根据 `resourceID`（layerid）**：不包含操作类型
- ✅ **逻辑锁区分操作类型**：`LockKey`（`lockType:resourceID`）用于逻辑锁

### 设计优势

1. **互斥保证**：同一镜像层的所有操作类型互斥，避免删除和下载并发造成错误
2. **并发性能**：不同镜像层可以并发操作，提升性能
3. **逻辑清晰**：分段锁保证互斥，逻辑锁区分操作类型

