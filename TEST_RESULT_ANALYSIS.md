# 测试结果分析

## 测试结果观察

### 问题现象

1. **节点A请求所有层时**：
   - 所有层都返回 `skip=true`（操作已完成）
   - 节点A跳过了所有层的下载

2. **节点B请求所有层时**：
   - 所有层都返回 `acquired=true`（获得锁）
   - 节点B开始下载所有层

### 问题分析

这个现象说明服务器端有**残留的锁状态**：

1. **节点A看到的状态**：
   - 服务器返回 `skip=true`，说明 `TryLock` 检查到 `lockInfo.Completed && lockInfo.Success`
   - 锁被标记为已完成，但可能还没有被清理

2. **节点B看到的状态**：
   - 服务器返回 `acquired=true`，说明 `TryLock` 没有找到锁
   - 锁可能已经被清理，或者节点B请求时锁状态已经改变

### 可能的原因

1. **锁清理时机问题**：
   - 当操作成功时，锁被标记为 `Completed=true, Success=true`
   - 锁不会被立即删除，而是保留给轮询的节点发现
   - 但在 `TryLock` 中，如果发现 `Completed && Success`，会删除锁并返回 `skip=true`
   - 这可能导致锁在节点A请求后被删除，节点B请求时锁已不存在

2. **并发竞争**：
   - 节点A和节点B几乎同时请求
   - 节点A先请求，发现锁已完成，删除锁，返回 `skip=true`
   - 节点B后请求，发现锁已不存在，创建新锁，返回 `acquired=true`

3. **残留状态**：
   - 之前的测试可能留下了已完成但未清理的锁状态
   - 节点A请求时发现了这些残留状态

## 解决方案

### 1. 使用唯一的层ID

每次测试使用唯一的层ID（加入时间戳），避免残留状态影响：

```go
timestamp := time.Now().Unix()
layers := []struct {
    ID       string
    Duration time.Duration
}{
    {fmt.Sprintf("sha256:layer1-%d", timestamp), 3 * time.Second},
    {fmt.Sprintf("sha256:layer2-%d", timestamp), 2 * time.Second},
    {fmt.Sprintf("sha256:layer3-%d", timestamp), 4 * time.Second},
    {fmt.Sprintf("sha256:layer4-%d", timestamp), 2 * time.Second},
}
```

### 2. 检查服务器端逻辑

服务器端的 `TryLock` 逻辑：

```go
if lockInfo.Completed {
    if lockInfo.Success {
        // 操作已完成且成功：清理锁，返回 skip=true
        delete(shard.locks, key)
        return false, true, "" // acquired=false, skip=true
    }
}
```

**问题**：当节点A发现锁已完成时，会删除锁。如果节点B在节点A删除锁之后请求，节点B会发现锁不存在，从而创建新锁。

### 3. 预期行为

**正确的行为应该是**：
- 节点A请求层1 → 获得锁 ✅
- 节点B请求层1 → 加入等待队列 ⏳
- 节点A完成层1 → 释放锁（成功）✅
- 节点B轮询发现层1已完成 → 跳过下载 ⏭️

**当前的行为**：
- 节点A请求层1 → 发现已完成，跳过 ⏭️
- 节点B请求层1 → 获得锁 ✅（不应该发生）

## 修复建议

### 方案1：确保测试使用唯一ID（已实现）

每次测试使用唯一的层ID，避免残留状态影响。

### 方案2：调整测试时序

确保节点A先开始下载，节点B稍后加入，模拟真实的并发场景。

### 方案3：检查服务器端清理逻辑

确保锁的清理逻辑正确，避免状态不一致。

## 测试验证

重新运行测试，应该看到：

1. **节点A**：
   - 获得所有层的锁 ✅
   - 开始下载所有层 ✅

2. **节点B**：
   - 请求所有层，加入等待队列 ⏳
   - 通过轮询发现层已完成，跳过下载 ⏭️
   - 或者从队列中获得锁，开始下载 ✅
