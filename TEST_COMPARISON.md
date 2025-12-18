# 测试代码对比

## 两个测试文件

### 1. `test-multi-node-multi-layer.go`（自己实现HTTP请求）

**特点**：
- 自己实现HTTP请求逻辑
- 自己实现轮询逻辑
- 可能不完全一致

**代码示例**：
```go
func requestLock(nodeID, layerID string) (*LockResponse, error) {
    req := LockRequest{...}
    resp, err := http.Post(serverURL+"/lock", ...)
    // 自己解析响应
}

func queryStatus(nodeID, layerID string) (*StatusResponse, error) {
    resp, err := http.Post(serverURL+"/lock/status", ...)
    // 自己解析响应和轮询逻辑
}
```

### 2. `test-client-multi-layer.go`（使用真实client库）✅ 推荐

**特点**：
- ✅ 直接使用 `client` 库的函数
- ✅ 使用 `client.NewLockClient()` 创建客户端
- ✅ 使用 `client.Lock()` 获取锁（包含重试、轮询等所有逻辑）
- ✅ 使用 `client.Unlock()` 释放锁
- ✅ 完全使用真实客户端的逻辑

**代码示例**：
```go
// 创建客户端
clientA := client.NewLockClient(serverURL, "NODEA")

// 使用真实的client库请求锁
request := &client.Request{
    Type:       client.OperationTypePull,
    ResourceID: layerID,
    NodeID:     nodeID,
}

// client库内部处理所有逻辑：重试、轮询等
result, err := clientA.Lock(ctx, request)
```

## 关键差异

| 特性 | test-multi-node-multi-layer.go | test-client-multi-layer.go |
|------|-------------------------------|---------------------------|
| HTTP请求 | 自己实现 | ✅ 使用client库 |
| 轮询逻辑 | 自己实现 | ✅ 使用client库 |
| 重试机制 | ❌ 没有 | ✅ 自动包含 |
| 错误处理 | 简单处理 | ✅ 完整处理 |
| 与真实客户端一致性 | ⚠️ 可能不一致 | ✅ 完全一致 |

## 推荐使用

**推荐使用 `test-client-multi-layer.go`**，因为：

1. ✅ **完全一致**：使用真实的client库，确保测试和实际使用一致
2. ✅ **自动包含**：自动包含重试机制、轮询机制等所有功能
3. ✅ **易于维护**：client库更新时，测试自动使用新逻辑
4. ✅ **真实场景**：更接近实际使用场景

## 运行测试

```bash
# 1. 启动服务器（终端1）
cd server
go run main.go

# 2. 运行测试（终端2）
# 使用真实client库的测试（推荐）
go run test-client-multi-layer.go

# 或者使用自己实现的测试
go run test-multi-node-multi-layer.go
```

