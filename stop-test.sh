#!/bin/bash
# stop-test.sh - 停止所有测试服务

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

echo "=========================================="
echo "停止测试服务"
echo "=========================================="
echo ""

# 停止 server
if [ -f test-data/logs/server.pid ]; then
    SERVER_PID=$(cat test-data/logs/server.pid)
    if ps -p $SERVER_PID > /dev/null 2>&1; then
        kill $SERVER_PID 2>/dev/null || true
        sleep 1
        if ps -p $SERVER_PID > /dev/null 2>&1; then
            kill -9 $SERVER_PID 2>/dev/null || true
        fi
        echo "✅ 已停止 server (PID: $SERVER_PID)"
    else
        echo "⚠️  Server 进程不存在 (PID: $SERVER_PID)"
    fi
    rm -f test-data/logs/server.pid
else
    echo "⚠️  未找到 server.pid 文件"
fi

# 停止各节点
for node in nodeA nodeB nodeC; do
    if [ -f test-data/logs/$node.pid ]; then
        NODE_PID=$(cat test-data/logs/$node.pid)
        if ps -p $NODE_PID > /dev/null 2>&1; then
            kill $NODE_PID 2>/dev/null || true
            sleep 1
            if ps -p $NODE_PID > /dev/null 2>&1; then
                kill -9 $NODE_PID 2>/dev/null || true
            fi
            echo "✅ 已停止 $node (PID: $NODE_PID)"
        else
            echo "⚠️  $node 进程不存在 (PID: $NODE_PID)"
        fi
        rm -f test-data/logs/$node.pid
    fi
done

# 强制清理残留进程
echo ""
echo "清理残留进程..."
pkill -f "lock-server" 2>/dev/null && echo "✅ 清理 lock-server 残留进程" || true
pkill -f "conchContent" 2>/dev/null && echo "✅ 清理 conchContent 残留进程" || true

# 清理 socket 文件
echo ""
echo "清理 socket 文件..."
rm -f test-data/sockets/*.sock
echo "✅ Socket 文件已清理"

echo ""
echo "=========================================="
echo "✅ 所有服务已停止"
echo "=========================================="
echo ""

