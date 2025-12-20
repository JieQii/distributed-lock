#!/bin/bash
# test-client-queue.sh - 测试客户端队列等待

echo "=========================================="
echo "测试客户端队列等待"
echo "=========================================="

# 检查服务器是否运行
if ! curl -s http://127.0.0.1:8080/lock/status > /dev/null 2>&1; then
    echo "❌ 错误: 服务器未运行，请先启动服务器"
    exit 1
fi

echo "✅ 服务器运行正常"
echo ""

RESOURCE_ID="sha256:queue-test-$(date +%s)"

# 节点A：获取锁并持有5秒
(
    cd client
    cat > /tmp/node-a.go << EOF
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
        ResourceID: "$RESOURCE_ID",
    }
    
    fmt.Println("[节点A] 请求锁...")
    result, err := c.Lock(ctx, req)
    if err != nil {
        fmt.Printf("[节点A] ❌ 错误: %v\n", err)
        return
    }
    
    if result.Acquired {
        fmt.Println("[节点A] ✅ 获得锁")
        fmt.Println("[节点A] 执行操作（5秒）...")
        time.Sleep(5 * time.Second)
        
        fmt.Println("[节点A] 释放锁...")
        req.Success = true
        if err := c.Unlock(ctx, req); err != nil {
            fmt.Printf("[节点A] ❌ 释放锁错误: %v\n", err)
        } else {
            fmt.Println("[节点A] ✅ 成功释放锁")
        }
    }
}
EOF
    go run /tmp/node-a.go
    rm -f /tmp/node-a.go
) &

NODE_A_PID=$!
sleep 1

# 节点B：请求锁（应该等待）
(
    cd client
    cat > /tmp/node-b.go << EOF
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
        ResourceID: "$RESOURCE_ID",
    }
    
    fmt.Println("[节点B] 请求锁...")
    start := time.Now()
    result, err := c.Lock(ctx, req)
    duration := time.Since(start)
    
    if err != nil {
        fmt.Printf("[节点B] ❌ 错误: %v\n", err)
        return
    }
    
    fmt.Printf("[节点B] 结果: acquired=%v, skipped=%v\n", result.Acquired, result.Skipped)
    fmt.Printf("[节点B] 等待时间: %v\n", duration)
    
    if result.Acquired {
        fmt.Println("[节点B] ✅ 获得锁（等待后）")
        fmt.Println("[节点B] 执行操作...")
        req.Success = true
        if err := c.Unlock(ctx, req); err != nil {
            fmt.Printf("[节点B] ❌ 释放锁错误: %v\n", err)
        } else {
            fmt.Println("[节点B] ✅ 成功释放锁")
        }
    } else if result.Skipped {
        fmt.Println("[节点B] ✅ 操作已跳过")
    }
}
EOF
    go run /tmp/node-b.go
    rm -f /tmp/node-b.go
) &

wait

echo ""
echo "=========================================="
echo "测试完成"
echo "=========================================="

