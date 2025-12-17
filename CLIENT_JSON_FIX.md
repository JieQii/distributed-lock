# 客户端 JSON 序列化修复说明

## 问题

测试 `test-client-basic.sh` 时出现错误：
```
❌ 错误: 服务器返回错误状态码: 400, 响应: 缺少必要参数
```

## 原因

客户端的 `Request` 结构体**缺少 JSON 标签**，导致序列化时使用大写字段名（`Type`, `ResourceID`, `NodeID`），但服务器端期望的是小写字段名（`type`, `resource_id`, `node_id`）。

### 修复前

```go
type Request struct {
    Type       string // 没有 JSON 标签，序列化为 "Type"
    ResourceID string // 没有 JSON 标签，序列化为 "ResourceID"
    NodeID     string // 没有 JSON 标签，序列化为 "NodeID"
    Success    bool   // 没有 JSON 标签，序列化为 "Success"
}
```

**序列化结果**：
```json
{
  "Type": "pull",
  "ResourceID": "sha256:test",
  "NodeID": "test-node",
  "Success": true
}
```

**服务器端期望**：
```json
{
  "type": "pull",
  "resource_id": "sha256:test",
  "node_id": "test-node",
  "success": true
}
```

### 修复后

```go
type Request struct {
    Type       string `json:"type"`        // ✅ 正确
    ResourceID string `json:"resource_id"` // ✅ 正确
    NodeID     string `json:"node_id"`     // ✅ 正确
    Error      string `json:"error,omitempty"` // ✅ 新增，用于解锁时传递错误信息
    Success    bool   `json:"success"`     // ✅ 正确
    Err        error  `json:"-"`           // 不序列化，仅内部使用
}
```

**序列化结果**：
```json
{
  "type": "pull",
  "resource_id": "sha256:test",
  "node_id": "test-node",
  "success": true
}
```

## 修复内容

### 1. 添加 JSON 标签

**文件**：`client/types.go`

```go
type Request struct {
    Type       string `json:"type"`        // ✅ 添加 JSON 标签
    ResourceID string `json:"resource_id"` // ✅ 添加 JSON 标签
    NodeID     string `json:"node_id"`     // ✅ 添加 JSON 标签
    Error      string `json:"error,omitempty"` // ✅ 新增字段，用于序列化错误信息
    Success    bool   `json:"success"`     // ✅ 添加 JSON 标签
    Err        error  `json:"-"`           // 不序列化，仅内部使用
}
```

### 2. 处理 Err 字段转换

**文件**：`client/client.go`

在 `tryUnlockOnce` 方法中，序列化前将 `Err` 转换为 `Error`：

```go
func (c *LockClient) tryUnlockOnce(ctx context.Context, request *Request) error {
    // 将 Err 转换为 Error 字符串（如果存在）
    if request.Err != nil && request.Error == "" {
        request.Error = request.Err.Error()
    }
    
    // 序列化请求
    jsonData, err := json.Marshal(request)
    // ...
}
```

## 验证修复

### 重新运行测试

```bash
# 1. 确保服务器运行
cd server
./lock-server

# 2. 运行客户端测试
cd ../client
go test -v

# 3. 运行集成测试脚本
cd ..
./test-client-basic.sh
```

### 预期结果

```
==========================================
测试客户端基本功能
==========================================
✅ 服务器运行正常

1. 请求锁...
2. 结果: acquired=true, skipped=false
3. 模拟操作（100ms）...
4. 释放锁...
✅ 成功释放锁
```

## 相关文件

- `client/types.go` - Request 结构体定义
- `client/client.go` - tryUnlockOnce 方法
- `server/handler.go` - Lock 和 Unlock 处理函数
- `server/types.go` - LockRequest 和 UnlockRequest 结构体

## 总结

修复了客户端 JSON 序列化问题：
- ✅ 添加了正确的 JSON 标签
- ✅ 添加了 Error 字段用于序列化错误信息
- ✅ 在序列化前将 Err 转换为 Error

现在客户端应该能够正常与服务器通信了。

