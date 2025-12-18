# 测试用例：节点在等待队列中时能够并发下载其他资源

## 测试目的

验证当节点B在等待队列中时，能够并发下载其他资源（不同的镜像层）。

## 测试场景

1. **节点A和节点B同时请求某个镜像层（layer1）**
   - 节点A获得锁
   - 节点B加入等待队列

2. **节点B再收到其他资源的请求（layer2）**
   - 节点B应该能够立即获得layer2的锁
   - 即使layer1还在等待队列中

3. **验证并发下载**
   - 节点B可以同时处理layer2的下载
   - layer1的等待队列状态不受影响

## 测试用例

### Go单元测试

**文件**: `server/lock_manager_test.go`

**测试函数**: `TestNodeConcurrentDifferentResources`

**测试步骤**:

1. 节点A请求layer1并获取锁 ✅
2. 节点B请求layer1，加入等待队列 ✅
3. 节点B请求layer2，立即获得锁 ✅（关键验证点）
4. 节点B完成layer2的操作，释放锁 ✅
5. 节点A完成layer1的操作，释放锁 ✅
6. 节点B从队列中获得layer1的锁 ✅

### Shell脚本测试

**文件**: `test-node-concurrent-layers.sh`

**使用方法**:

```bash
# 1. 启动服务器
cd server
go run main.go

# 2. 在另一个终端运行测试脚本
cd ..
./test-node-concurrent-layers.sh
```

## 测试结果

### Go单元测试结果

```
=== RUN   TestNodeConcurrentDifferentResources
    lock_manager_test.go:470: ✅ 节点A获得layer1的锁
    lock_manager_test.go:482: ✅ 节点B加入layer1的等待队列
    lock_manager_test.go:489: ✅ layer1的队列长度为1（节点B在队列中）
    lock_manager_test.go:501: ✅ 节点B获得layer2的锁（即使layer1还在等待队列中）
    lock_manager_test.go:513: ✅ 节点B持有layer2的锁，layer1的队列长度仍为1
    lock_manager_test.go:526: ✅ 节点B释放layer2的锁
    lock_manager_test.go:545: ✅ 节点A释放layer1的锁（操作失败）
    lock_manager_test.go:554: ✅ 节点B从队列中获得layer1的锁
    lock_manager_test.go:561: ✅ layer1的队列已清空
    lock_manager_test.go:563: ✅ 测试通过：节点B在等待layer1时，能够并发下载layer2
--- PASS: TestNodeConcurrentDifferentResources (0.00s)
PASS
```

### 关键验证点 ✅

1. **节点B在等待队列中时，能够并发获得其他资源的锁** ✅
   - 节点B请求layer2时，立即获得锁
   - 即使layer1还在等待队列中

2. **不同资源的锁是独立的** ✅
   - layer1的等待队列状态不受影响
   - layer2的锁获取不影响layer1的队列

3. **队列机制正常工作** ✅
   - 节点A释放锁后，节点B从队列中获得锁
   - 队列正确清空

## 实现原理

### 锁的粒度

锁的粒度是 `type:resource_id`，这意味着：

- **同一个资源**（相同的 `resource_id`）只能被一个节点持有锁
- **不同的资源**（不同的 `resource_id`）可以被不同的节点并发持有锁

### 代码实现

```go
// server/types.go
func LockKey(lockType, resourceID string) string {
    return lockType + ":" + resourceID
}
```

**结果**：
- `pull:sha256:layer1` 和 `pull:sha256:layer2` 是不同的锁
- 节点B可以同时持有 `pull:sha256:layer2` 的锁，同时等待 `pull:sha256:layer1` 的锁

### 分段锁机制

```go
// server/lock_manager.go
const shardCount = 32

func (lm *LockManager) getShard(key string) *resourceShard {
    h := fnv.New32a()
    h.Write([]byte(key))
    return lm.shards[h.Sum32()%shardCount]
}
```

**优势**：
- 不同资源可能落在不同的分段中
- 不同分段的锁可以并发获取
- 提高并发性能

## 使用场景

### 实际应用场景

1. **镜像下载**
   - 节点B需要下载镜像的多个层
   - layer1被节点A占用，节点B加入等待队列
   - 节点B可以同时下载layer2、layer3等其他层
   - 提高下载效率

2. **资源管理**
   - 节点可以同时处理多个不同的资源
   - 不会因为一个资源的等待而阻塞其他资源的处理

## 总结

### ✅ 测试通过

- **功能正确**：节点在等待队列中时，能够并发下载其他资源
- **实现正确**：不同资源的锁是独立的，可以并发获取
- **队列机制正常**：等待队列不影响其他资源的处理

### 关键点

1. **锁的粒度**：`type:resource_id`
2. **并发性**：不同资源可以并发处理
3. **独立性**：每个资源的锁和队列是独立的

### 运行测试

```bash
# Go单元测试
cd server
go test -v -run TestNodeConcurrentDifferentResources

# Shell脚本测试（需要先启动服务器）
./test-node-concurrent-layers.sh
```

