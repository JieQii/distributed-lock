# 仲裁逻辑（当前设计）

> 服务器仲裁只处理“锁是否可占用”和“排队”，**不再基于引用计数或业务规则决定跳过/拒绝**。业务侧（Content 插件）在获取锁前后自行用 `callback` 判断。

## 核心规则

1. 同一资源键（`Type:ResourceID`）同一时间仅一个 holder。
2. 已有锁且未完成 → 新请求进入 FIFO 等待队列。
3. 已有锁且已完成 → 清理锁，若队列有请求则分配队头。
4. 无锁 → 直接获得锁。
5. `skip` 字段仅为兼容保留，服务器始终返回 `skip=false`。

> 注：当前实现是“单 FIFO 队列/资源键”。若未来需要按 subtype 拆分队列，可在 processQueue 中按 subtype 选择队头，逻辑类似但队列存储结构需扩展。

## TryLock 伪代码

```go
key := LockKey(req.Type, req.ResourceID)
shard := getShard(key)
shard.mu.Lock()
defer shard.mu.Unlock()

if lock, exists := shard.locks[key]; exists {
    if lock.Completed {
        delete(shard.locks, key)
        processQueue(shard, key) // 若有队头则占位
    } else {
        addToQueue(shard, key, req)
        return acquired=false, skip=false, ""
    }
}

// 无锁，占位
shard.locks[key] = &LockInfo{Request: req, AcquiredAt: now()}
return acquired=true, skip=false, ""
```

## Unlock 伪代码

```go
key := LockKey(req.Type, req.ResourceID)
shard := getShard(key)
shard.mu.Lock()
defer shard.mu.Unlock()

lock, ok := shard.locks[key]
if !ok || lock.Request.NodeID != req.NodeID {
    return false
}

delete(shard.locks, key)
processQueue(shard, key) // 分配队头为新锁（若存在）
return true
```

## 业务层与引用计数

- 服务器不再检查或维护引用计数；也无 `/refcount` 接口。
- Content 插件应在获取锁前调用 `callback.ShouldSkipOperation`，在成功后调用 `callback.UpdateRefCount`，并自行持久化计数（文件/DB）。

## 兼容性说明

- 客户端/协议中的 `skip` 字段可保留用于向后兼容，但当前服务器实现不会置为 true。业务侧应自行判断“做/不做”。

