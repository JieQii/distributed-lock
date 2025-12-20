# 客户端调试快速开始

## 前置条件

1. **服务器必须运行**
```bash
cd server
./lock-server
# 或后台运行
./lock-server > ../test-data/logs/server.log 2>&1 &
```

2. **验证服务器**
```bash
curl http://127.0.0.1:8080/lock/status
```

---

## 快速测试步骤

### 步骤1：运行单元测试

```bash
cd client
go test -v
```

**预期输出**：
```
=== RUN   TestConcurrentPullOperations
    client_test.go:74: 并发pull操作完成，成功获取锁的节点数: 10
--- PASS: TestConcurrentPullOperations (0.12s)
PASS
```

### 步骤2：测试基本功能

```bash
# 给脚本添加执行权限（Linux）
chmod +x test-client-basic.sh

# 运行基本功能测试
./test-client-basic.sh
```

**预期输出**：
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

### 步骤3：测试队列等待

```bash
chmod +x test-client-queue.sh
./test-client-queue.sh
```

**预期输出**：
```
[节点A] ✅ 获得锁
[节点A] 执行操作（5秒）...
[节点B] 请求锁...
[节点B] 结果: acquired=true, skipped=false
[节点B] 等待时间: ~5秒
[节点B] ✅ 获得锁（等待后）
```

### 步骤4：测试轮询机制

```bash
chmod +x test-client-polling.sh
./test-client-polling.sh
```

**预期输出**：
```
[节点A] ✅ 获得锁
[节点A] 执行操作（2秒）...
[节点A] ✅ 成功释放锁
[节点B] 请求锁...
[节点B] 结果: acquired=false, skipped=true
[节点B] ✅ 正确跳过操作（通过轮询发现操作已完成）
```

---

## 详细调试步骤

### 1. 运行单元测试

```bash
cd client

# 运行所有测试
go test -v

# 运行特定测试
go test -v -run TestConcurrentPullOperations
go test -v -run TestDeleteWithReferences
go test -v -run TestRetryMechanism
go test -v -run TestTimeout

# 查看覆盖率
go test -v -cover
```

### 2. 调试单个功能

#### 2.1 添加日志

在 `client/client.go` 中添加日志：

```go
import "log"

func (c *LockClient) Lock(ctx context.Context, request *Request) (*LockResult, error) {
    log.Printf("[DEBUG] 请求锁: type=%s, resource=%s", request.Type, request.ResourceID)
    // ... 原有代码 ...
}
```

#### 2.2 使用调试器

```bash
# 安装 Delve（如果还没有）
go install github.com/go-delve/delve/cmd/dlv@latest

# 调试测试
cd client
dlv test -- -test.run TestConcurrentPullOperations

# 在 Delve 中：
# (dlv) break client.go:50
# (dlv) continue
# (dlv) print request
```

### 3. 测试不同场景

#### 场景1：正常流程
```bash
./test-client-basic.sh
```

#### 场景2：队列等待
```bash
./test-client-queue.sh
```

#### 场景3：轮询发现操作已完成
```bash
./test-client-polling.sh
```

#### 场景4：并发测试
```bash
cd client
go test -v -run TestConcurrentPullOperations
```

### 4. 集成测试（连接真实服务器）

创建 `client/integration_test.go`：

```go
package client

import (
    "context"
    "testing"
)

func TestIntegrationWithRealServer(t *testing.T) {
    if testing.Short() {
        t.Skip("跳过集成测试")
    }

    client := NewLockClient("http://127.0.0.1:8080", "test-node")
    ctx := context.Background()
    
    req := &Request{
        Type:       OperationTypePull,
        ResourceID: "sha256:integration-test",
    }
    
    result, err := client.Lock(ctx, req)
    if err != nil {
        t.Fatalf("获取锁失败: %v", err)
    }
    
    if !result.Acquired {
        t.Fatal("期望获得锁")
    }
    
    req.Success = true
    if err := client.Unlock(ctx, req); err != nil {
        t.Fatalf("释放锁失败: %v", err)
    }
}
```

运行：
```bash
go test -v -run TestIntegrationWithRealServer
```

---

## 常见问题排查

### 问题1：无法连接服务器

```bash
# 检查服务器是否运行
curl http://127.0.0.1:8080/lock/status

# 检查端口
netstat -tlnp | grep :8080

# 检查防火墙
sudo iptables -L | grep 8080
```

### 问题2：测试超时

```bash
# 增加超时时间
client.RequestTimeout = 60 * time.Second
```

### 问题3：重试失败

```bash
# 增加重试次数
client.MaxRetries = 5
client.RetryInterval = 2 * time.Second
```

---

## 测试检查清单

- [ ] 单元测试全部通过
- [ ] 基本功能测试通过
- [ ] 队列等待功能正常
- [ ] 轮询机制正常（操作已完成时跳过）
- [ ] 重试机制正常
- [ ] 超时处理正常
- [ ] 错误处理正常
- [ ] 并发安全性正常

---

## 下一步

完成客户端调试后：
1. 测试 conchContent-v3 的完整流程
2. 测试多节点场景
3. 测试引用计数功能
4. 进行压力测试

详细文档请参考：`CLIENT_DEBUGGING_GUIDE.md`

