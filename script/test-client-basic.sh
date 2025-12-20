#!/bin/bash
# test-client-basic.sh - 测试客户端基本功能

echo "=========================================="
echo "测试客户端基本功能"
echo "=========================================="

# 检查服务器是否运行
if ! curl -s http://127.0.0.1:8080/lock/status > /dev/null 2>&1; then
    echo "❌ 错误: 服务器未运行，请先启动服务器"
    echo "   运行: cd server && ./lock-server"
    exit 1
fi

echo "✅ 服务器运行正常"
echo ""

# 创建临时测试程序
cd client
cat > /tmp/test-client-basic.go << 'EOF'
package main

import (
    "context"
    "fmt"
    "time"
    "distributed-lock/client"
)

func main() {
    c := client.NewLockClient("http://127.0.0.1:8080", "test-node-basic")
    ctx := context.Background()
    
    resourceID := "sha256:client-basic-test-" + fmt.Sprintf("%d", time.Now().Unix())
    
    req := &client.Request{
        Type:       client.OperationTypePull,
        ResourceID: resourceID,
    }
    
    fmt.Println("1. 请求锁...")
    result, err := c.Lock(ctx, req)
    if err != nil {
        fmt.Printf("❌ 错误: %v\n", err)
        return
    }
    
    fmt.Printf("2. 结果: acquired=%v, skipped=%v\n", result.Acquired, result.Skipped)
    
    if result.Acquired {
        fmt.Println("3. 模拟操作（100ms）...")
        time.Sleep(100 * time.Millisecond)
        
        fmt.Println("4. 释放锁...")
        req.Success = true
        if err := c.Unlock(ctx, req); err != nil {
            fmt.Printf("❌ 释放锁错误: %v\n", err)
        } else {
            fmt.Println("✅ 成功释放锁")
        }
    } else if result.Skipped {
        fmt.Println("✅ 操作已跳过（资源已存在）")
    } else {
        fmt.Println("⚠️  未获得锁，也未跳过")
    }
}
EOF

go run /tmp/test-client-basic.go
rm -f /tmp/test-client-basic.go

echo ""
echo "=========================================="
echo "测试完成"
echo "=========================================="

