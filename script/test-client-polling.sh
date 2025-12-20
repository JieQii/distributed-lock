#!/bin/bash
# test-client-polling.sh - 测试客户端轮询机制（操作已完成）

echo "=========================================="
echo "测试客户端轮询机制（操作已完成）"
echo "=========================================="

# 检查服务器是否运行
if ! curl -s http://127.0.0.1:8080/lock/status > /dev/null 2>&1; then
    echo "❌ 错误: 服务器未运行，请先启动服务器"
    exit 1
fi

echo "✅ 服务器运行正常"
echo ""

RESOURCE_ID="sha256:polling-test-$(date +%s)"

# 节点A：快速完成操作
(
    cd client
    cat > /tmp/node-a-poll.go << EOF
package main

import (
    "context"
    "fmt"
    "time"
    "distributed-lock/client"
)

func main() {
    c := client.NewLockClient("http://127.0.0.1:8080", "node-a-poll")
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
        fmt.Println("[节点A] 执行操作（2秒）...")
        time.Sleep(2 * time.Second)
        
        fmt.Println("[节点A] 释放锁（成功）...")
        req.Success = true
        if err := c.Unlock(ctx, req); err != nil {
            fmt.Printf("[节点A] ❌ 释放锁错误: %v\n", err)
        } else {
            fmt.Println("[节点A] ✅ 成功释放锁")
        }
    }
}
EOF
    go run /tmp/node-a-poll.go
    rm -f /tmp/node-a-poll.go
) &

sleep 1

# 节点B：请求锁（应该通过轮询发现操作已完成）
(
    cd client
    cat > /tmp/node-b-poll.go << EOF
package main

import (
    "context"
    "fmt"
    "time"
    "distributed-lock/client"
)

func main() {
    c := client.NewLockClient("http://127.0.0.1:8080", "node-b-poll")
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
    
    if result.Skipped {
        fmt.Println("[节点B] ✅ 正确跳过操作（通过轮询发现操作已完成）")
    } else if result.Acquired {
        fmt.Println("[节点B] ⚠️  获得锁（操作未完成）")
        req.Success = true
        c.Unlock(ctx, req)
    } else {
        fmt.Println("[节点B] ⚠️  未获得锁，也未跳过")
    }
}
EOF
    go run /tmp/node-b-poll.go
    rm -f /tmp/node-b-poll.go
) &

wait

echo ""
echo "=========================================="
echo "测试完成"
echo "=========================================="
echo ""
echo "预期结果："
echo "  - 节点B应该通过轮询发现操作已完成"
echo "  - 节点B应该跳过操作（skipped=true）"
echo "  - 等待时间应该接近节点A的操作时间（2秒）"

