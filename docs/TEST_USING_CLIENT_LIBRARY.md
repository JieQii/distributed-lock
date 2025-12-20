# 使用真实Client库的测试

## 文件说明

**`test-client-multi-layer.go`** - 直接使用 `client` 库的测试程序

## 与之前测试的区别

### 之前的测试 (`test-multi-node-multi-layer.go`)

- ❌ 自己实现了HTTP请求逻辑
- ❌ 自己实现了轮询逻辑
- ❌ 可能与真实客户端不一致

### 新的测试 (`test-client-multi-layer.go`)

- ✅ 直接使用 `client` 库的函数
- ✅ 使用 `client.NewLockClient()` 创建客户端
- ✅ 使用 `client.Lock()` 获取锁
- ✅ 使用 `client.Unlock()` 释放锁
- ✅ 完全使用真实客户端的逻辑

## 代码对比

### 之前的测试代码

```go
// 自己实现HTTP请求
func requestLock(nodeID, layerID string) (*LockResponse, error) {
    req := LockRequest{...}
    resp, err := http.Post(serverURL+"/lock", ...)
    // ... 自己解析响应
}

// 自己实现轮询
func queryStatus(nodeID, layerID string) (*StatusResponse, error) {
    resp, err := http.Post(serverURL+"/lock/status", ...)
    // ... 自己解析响应
}
```

### 新的测试代码

```go
// 使用真实的client库
clientA := client.NewLockClient(serverURL, "NODEA")
request := &client.Request{
    Type:       client.OperationTypePull,
    ResourceID: layerID,
    NodeID:     nodeID,
}
result, err := clientA.Lock(ctx, request)
// client库内部处理所有逻辑：重试、轮询等
```

## 优势

1. **完全一致**：使用真实的client库，确保测试和实际使用一致
2. **自动包含**：自动包含重试机制、轮询机制等所有功能
3. **易于维护**：client库更新时，测试自动使用新逻辑
4. **真实场景**：更接近实际使用场景

## 运行测试

```bash
# 1. 启动服务器（终端1）
cd server
go run main.go

# 2. 运行测试（终端2）
go run test-client-multi-layer.go
```

## 测试场景

- 节点A和节点B同时请求下载四个镜像层
- 节点A先获得所有锁，开始下载
- 节点B加入等待队列，通过轮询发现操作已完成，跳过下载

## 预期结果

1. 节点A获得所有层的锁，开始并发下载
2. 节点B加入等待队列，开始轮询
3. 节点A完成下载后，节点B通过轮询发现操作已完成，跳过下载

