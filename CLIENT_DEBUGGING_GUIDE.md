# 客户端调试详细指南

## 概述

客户端调试分为两个部分：
1. **基础客户端** (`client/`) - 独立的客户端库
2. **conchContent-v3 客户端** (`conchContent-v3/lockclient/`) - 集成在 conchContent-v3 中的客户端

---

## 第一步：准备环境

### 1.1 确保服务器正在运行

```bash
# 在第一个终端启动服务器
cd server
./lock-server

# 或者后台运行
./lock-server > ../test-data/logs/server.log 2>&1 &
echo $! > ../test-data/logs/server.pid

# 验证服务器是否运行
curl http://127.0.0.1:8080/lock/status
```

### 1.2 检查服务器状态

```bash
# 检查端口是否监听
netstat -tlnp | grep :8080
# 或
ss -tlnp | grep :8080

# 测试服务器健康
curl -X POST http://127.0.0.1:8080/lock \
  -H "Content-Type: application/json" \
  -d '{"type":"pull","resource_id":"sha256:test","node_id":"TEST"}'
```

---

## 第二步：运行单元测试

### 2.1 运行基础客户端测试

```bash
# 进入客户端目录
cd client

# 运行所有测试
go test -v

# 运行特定测试
go test -v -run TestConcurrentPullOperations
go test -v -run TestDeleteWithReferences
go test -v -run TestRetryMechanism
go test -v -run TestTimeout

# 运行测试并查看覆盖率
go test -v -cover
go test -v -coverprofile=coverage.out
go tool cover -html=coverage.out
```

### 2.2 测试输出说明

**成功输出示例**：
```
=== RUN   TestConcurrentPullOperations
    client_test.go:74: 并发pull操作完成，成功获取锁的节点数: 10
--- PASS: TestConcurrentPullOperations (0.12s)
PASS
```

**失败输出示例**：
```
=== RUN   TestTimeout
    client_test.go:183: 正确返回超时错误: 请求超时: context deadline exceeded
--- PASS: TestTimeout (0.50s)
PASS
```

---

## 第三步：编写集成测试（连接真实服务器）

### 3.1 创建集成测试文件

创建 `client/integration_test.go`：

```go
package client

import (
    "context"
    "testing"
    "time"
)

// TestIntegrationWithRealServer 集成测试（需要真实服务器）
func TestIntegrationWithRealServer(t *testing.T) {
    // 跳过测试，除非设置了环境变量
    if testing.Short() {
        t.Skip("跳过集成测试")
    }

    // 创建客户端（连接到真实服务器）
    client := NewLockClient("http://127.0.0.1:8080", "test-node-integration")
    
    ctx := context.Background()
    resourceID := "sha256:integration-test-" + time.Now().Format("20060102150405")
    
    // 测试获取锁
    request := &Request{
        Type:       OperationTypePull,
        ResourceID: resourceID,
    }
    
    result, err := client.Lock(ctx, request)
    if err != nil {
        t.Fatalf("获取锁失败: %v", err)
    }
    
    if !result.Acquired {
        t.Fatal("期望获得锁，但没有获得")
    }
    
    t.Logf("成功获得锁: %+v", result)
    
    // 测试释放锁
    request.Success = true
    if err := client.Unlock(ctx, request); err != nil {
        t.Fatalf("释放锁失败: %v", err)
    }
    
    t.Log("成功释放锁")
}
```

### 3.2 运行集成测试

```bash
# 运行集成测试（需要真实服务器）
cd client
go test -v -run TestIntegrationWithRealServer

# 跳过集成测试（只运行单元测试）
go test -v -short
```

---

## 第四步：调试客户端代码

### 4.1 使用日志调试

在客户端代码中添加日志：

```go
// 在 client/client.go 中添加日志
import "log"

func (c *LockClient) Lock(ctx context.Context, request *Request) (*LockResult, error) {
    log.Printf("[DEBUG] 请求锁: type=%s, resource=%s, node=%s", 
        request.Type, request.ResourceID, c.NodeID)
    
    // ... 原有代码 ...
    
    result, err := c.tryLockOnce(ctx, request)
    if err == nil {
        log.Printf("[DEBUG] 获取锁成功: acquired=%v, skipped=%v", 
            result.Acquired, result.Skipped)
        return result, nil
    }
    
    log.Printf("[DEBUG] 获取锁失败: %v", err)
    // ... 原有代码 ...
}
```

### 4.2 使用调试器（Delve）

```bash
# 安装 Delve（如果还没有）
go install github.com/go-delve/delve/cmd/dlv@latest

# 使用 Delve 调试测试
cd client
dlv test -- -test.run TestConcurrentPullOperations

# 在 Delve 中设置断点
# (dlv) break client.go:50
# (dlv) continue
# (dlv) print request
# (dlv) next
```

### 4.3 使用打印语句调试

创建测试脚本 `client/debug_test.go`：

```go
package client

import (
    "context"
    "fmt"
    "time"
)

func ExampleDebugLock() {
    client := NewLockClient("http://127.0.0.1:8080", "debug-node")
    ctx := context.Background()
    
    request := &Request{
        Type:       OperationTypePull,
        ResourceID: "sha256:debug-test",
    }
    
    fmt.Println("1. 请求锁...")
    result, err := client.Lock(ctx, request)
    if err != nil {
        fmt.Printf("错误: %v\n", err)
        return
    }
    
    fmt.Printf("2. 结果: acquired=%v, skipped=%v\n", result.Acquired, result.Skipped)
    
    if result.Acquired {
        fmt.Println("3. 模拟操作...")
        time.Sleep(100 * time.Millisecond)
        
        fmt.Println("4. 释放锁...")
        request.Success = true
        if err := client.Unlock(ctx, request); err != nil {
            fmt.Printf("释放锁错误: %v\n", err)
        } else {
            fmt.Println("5. 成功释放锁")
        }
    }
}
```

运行：
```bash
cd client
go run -exec go run debug_test.go
```

---

## 第五步：测试不同场景

### 5.1 场景1：正常获取和释放锁

创建测试脚本 `test-client-basic.sh`：

```bash
#!/bin/bash
# test-client-basic.sh - 测试客户端基本功能

echo "=========================================="
echo "测试客户端基本功能"
echo "=========================================="

# 编译测试程序
cd client
go build -o test-client test-client.go 2>/dev/null || {
    echo "创建测试程序..."
    cat > test-client.go << 'EOF'
package main

import (
    "context"
    "fmt"
    "time"
    "distributed-lock/client"
)

func main() {
    c := client.NewLockClient("http://127.0.0.1:8080", "test-node")
    ctx := context.Background()
    
    req := &client.Request{
        Type:       client.OperationTypePull,
        ResourceID: "sha256:client-test",
    }
    
    fmt.Println("请求锁...")
    result, err := c.Lock(ctx, req)
    if err != nil {
        fmt.Printf("错误: %v\n", err)
        return
    }
    
    fmt.Printf("结果: acquired=%v, skipped=%v\n", result.Acquired, result.Skipped)
    
    if result.Acquired {
        fmt.Println("模拟操作...")
        time.Sleep(100 * time.Millisecond)
        
        fmt.Println("释放锁...")
        req.Success = true
        if err := c.Unlock(ctx, req); err != nil {
            fmt.Printf("释放锁错误: %v\n", err)
        } else {
            fmt.Println("成功释放锁")
        }
    }
}
EOF
    go build -o test-client test-client.go
}

./test-client
```

### 5.2 场景2：测试队列等待

创建测试脚本 `test-client-queue.sh`：

```bash
#!/bin/bash
# test-client-queue.sh - 测试客户端队列等待

echo "=========================================="
echo "测试客户端队列等待"
echo "=========================================="

# 创建两个客户端同时请求锁
(
    cd client
    go run -exec 'go run' << 'EOF' &
package main

import (
    "context"
    "fmt"
    "time"
    "distributed-lock/client"
)

func main() {
    c := client.NewLockClient("http://127.0.0.1:8080", "node-a")
    ctx := context.Background()
    
    req := &client.Request{
        Type:       client.OperationTypePull,
        ResourceID: "sha256:queue-test",
    }
    
    fmt.Println("[节点A] 请求锁...")
    result, err := c.Lock(ctx, req)
    if err != nil {
        fmt.Printf("[节点A] 错误: %v\n", err)
        return
    }
    
    fmt.Printf("[节点A] 获得锁: acquired=%v\n", result.Acquired)
    
    if result.Acquired {
        fmt.Println("[节点A] 执行操作（5秒）...")
        time.Sleep(5 * time.Second)
        
        fmt.Println("[节点A] 释放锁...")
        req.Success = true
        c.Unlock(ctx, req)
    }
}
EOF
)

sleep 1

(
    cd client
    go run -exec 'go run' << 'EOF'
package main

import (
    "context"
    "fmt"
    "time"
    "distributed-lock/client"
)

func main() {
    c := client.NewLockClient("http://127.0.0.1:8080", "node-b")
    ctx := context.Background()
    
    req := &client.Request{
        Type:       client.OperationTypePull,
        ResourceID: "sha256:queue-test",
    }
    
    fmt.Println("[节点B] 请求锁...")
    start := time.Now()
    result, err := c.Lock(ctx, req)
    duration := time.Since(start)
    
    if err != nil {
        fmt.Printf("[节点B] 错误: %v\n", err)
        return
    }
    
    fmt.Printf("[节点B] 获得锁: acquired=%v, 等待时间: %v\n", result.Acquired, duration)
    
    if result.Acquired {
        fmt.Println("[节点B] 执行操作...")
        req.Success = true
        c.Unlock(ctx, req)
    }
}
EOF
)

wait
```

### 5.3 场景3：测试轮询机制（操作已完成）

创建测试脚本 `test-client-polling.sh`：

```bash
#!/bin/bash
# test-client-polling.sh - 测试客户端轮询机制

echo "=========================================="
echo "测试客户端轮询机制（操作已完成）"
echo "=========================================="

# 节点A先获取锁并完成操作
(
    cd client
    go run -exec 'go run' << 'EOF' &
package main

import (
    "context"
    "fmt"
    "time"
    "distributed-lock/client"
)

func main() {
    c := client.NewLockClient("http://127.0.0.1:8080", "node-a")
    ctx := context.Background()
    
    req := &client.Request{
        Type:       client.OperationTypePull,
        ResourceID: "sha256:polling-test",
    }
    
    fmt.Println("[节点A] 请求锁...")
    result, err := c.Lock(ctx, req)
    if err != nil {
        fmt.Printf("[节点A] 错误: %v\n", err)
        return
    }
    
    if result.Acquired {
        fmt.Println("[节点A] 执行操作（2秒）...")
        time.Sleep(2 * time.Second)
        
        fmt.Println("[节点A] 释放锁（成功）...")
        req.Success = true
        c.Unlock(ctx, req)
    }
}
EOF
)

sleep 1

# 节点B请求锁（应该通过轮询发现操作已完成）
(
    cd client
    go run -exec 'go run' << 'EOF'
package main

import (
    "context"
    "fmt"
    "time"
    "distributed-lock/client"
)

func main() {
    c := client.NewLockClient("http://127.0.0.1:8080", "node-b")
    ctx := context.Background()
    
    req := &client.Request{
        Type:       client.OperationTypePull,
        ResourceID: "sha256:polling-test",
    }
    
    fmt.Println("[节点B] 请求锁...")
    start := time.Now()
    result, err := c.Lock(ctx, req)
    duration := time.Since(start)
    
    if err != nil {
        fmt.Printf("[节点B] 错误: %v\n", err)
        return
    }
    
    fmt.Printf("[节点B] 结果: acquired=%v, skipped=%v, 等待时间: %v\n", 
        result.Acquired, result.Skipped, duration)
    
    if result.Skipped {
        fmt.Println("[节点B] ✅ 正确跳过操作（发现操作已完成）")
    } else if result.Acquired {
        fmt.Println("[节点B] 获得锁（操作未完成）")
        req.Success = true
        c.Unlock(ctx, req)
    }
}
EOF
)

wait
```

---

## 第六步：调试 conchContent-v3 客户端

### 6.1 测试 conchContent-v3 客户端

```bash
# 进入 conchContent-v3 目录
cd conchContent-v3

# 运行 conchContent-v3（会自动使用客户端）
./conchContent -config ../test-data/config-nodeA.toml

# 在另一个终端测试
curl -X POST http://127.0.0.1:8080/lock \
  -H "Content-Type: application/json" \
  -d '{"type":"pull","resource_id":"sha256:test","node_id":"NODEA"}'
```

### 6.2 调试 conchContent-v3 客户端代码

在 `conchContent-v3/lockclient/client.go` 中添加日志：

```go
import "log"

func (c *LockClient) waitForLock(ctx context.Context, request *Request) (*LockResult, error) {
    log.Printf("[DEBUG] 进入 waitForLock，开始轮询...")
    ticker := time.NewTicker(500 * time.Millisecond)
    defer ticker.Stop()

    for {
        select {
        case <-ctx.Done():
            return nil, ctx.Err()
        case <-ticker.C:
            log.Printf("[DEBUG] 轮询查询锁状态...")
            // ... 原有代码 ...
            
            if statusResp.Completed && statusResp.Success {
                log.Printf("[DEBUG] 发现操作已完成且成功，跳过操作")
                return &LockResult{
                    Acquired: false,
                    Skipped:  true,
                }, nil
            }
        }
    }
}
```

---

## 第七步：常见问题排查

### 7.1 客户端无法连接服务器

```bash
# 检查服务器是否运行
curl http://127.0.0.1:8080/lock/status

# 检查网络连接
telnet 127.0.0.1 8080

# 检查防火墙
sudo iptables -L | grep 8080
```

### 7.2 客户端超时

```bash
# 增加超时时间
client := NewLockClient("http://127.0.0.1:8080", "test-node")
client.RequestTimeout = 60 * time.Second
```

### 7.3 客户端重试失败

```bash
# 增加重试次数
client.MaxRetries = 5
client.RetryInterval = 2 * time.Second
```

### 7.4 查看详细日志

```bash
# 启用详细日志
export DEBUG=1
go test -v -run TestConcurrentPullOperations
```

---

## 第八步：性能测试

### 8.1 并发性能测试

创建 `client/benchmark_test.go`：

```go
package client

import (
    "context"
    "testing"
    "time"
)

func BenchmarkLockUnlock(b *testing.B) {
    client := NewLockClient("http://127.0.0.1:8080", "bench-node")
    ctx := context.Background()
    
    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        req := &Request{
            Type:       OperationTypePull,
            ResourceID: "sha256:bench-" + string(rune(i)),
        }
        
        result, err := client.Lock(ctx, req)
        if err != nil {
            b.Fatal(err)
        }
        
        if result.Acquired {
            req.Success = true
            client.Unlock(ctx, req)
        }
    }
}
```

运行：
```bash
cd client
go test -bench=BenchmarkLockUnlock -benchmem
```

---

## 快速参考

### 常用命令

```bash
# 运行所有测试
cd client && go test -v

# 运行特定测试
go test -v -run TestName

# 运行集成测试（需要真实服务器）
go test -v -run Integration

# 查看测试覆盖率
go test -v -cover

# 调试测试
dlv test -- -test.run TestName

# 性能测试
go test -bench=. -benchmem
```

### 测试检查清单

- [ ] 单元测试全部通过
- [ ] 集成测试通过（连接真实服务器）
- [ ] 队列等待功能正常
- [ ] 轮询机制正常（操作已完成时跳过）
- [ ] 重试机制正常
- [ ] 超时处理正常
- [ ] 错误处理正常
- [ ] 并发安全性正常

---

## 下一步

完成客户端调试后，可以：
1. 测试 conchContent-v3 的完整流程
2. 测试多节点场景
3. 测试引用计数功能
4. 进行压力测试

