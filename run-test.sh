#!/bin/bash
# run-test.sh - 一键启动测试环境

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

echo "=========================================="
echo "启动 conchContent-v3 和 server 测试环境"
echo "=========================================="
echo ""

# 编译 server
echo "[1/5] 编译 server..."
cd server
if ! go build -o lock-server .; then
    echo "❌ 编译 server 失败"
    exit 1
fi
echo "✅ server 编译成功"

# 编译 conchContent-v3
echo ""
echo "[2/5] 编译 conchContent-v3..."
cd ../conchContent-v3
if ! go build -o conchContent .; then
    echo "❌ 编译 conchContent-v3 失败"
    exit 1
fi
echo "✅ conchContent-v3 编译成功"

cd ..

# 创建目录结构
echo ""
echo "[3/5] 创建测试目录..."
mkdir -p test-data/{nodeA,nodeB,nodeC}/{host,merged}/{blobs/sha256,ingest}
mkdir -p test-data/{logs,sockets}
echo "✅ 目录创建完成"

# 检查配置文件
echo ""
echo "[4/5] 检查配置文件..."
if [ ! -f test-data/config-nodeA.toml ]; then
    echo "⚠️  配置文件不存在，正在创建..."
    cat > test-data/config-nodeA.toml << 'EOF'
current_node = "NODEA"

socket_path = "test-data/sockets/conch-a.sock"

[nodes.NODEA]
root        = "test-data/nodeA"
ip          = "127.0.0.1"
lock_server = "http://127.0.0.1:8080"

[nodes.NODEB]
root        = "test-data/nodeB"
ip          = "127.0.0.1"
lock_server = "http://127.0.0.1:8080"

[nodes.NODEC]
root        = "test-data/nodeC"
ip          = "127.0.0.1"
lock_server = "http://127.0.0.1:8080"
EOF

    cat > test-data/config-nodeB.toml << 'EOF'
current_node = "NODEB"

socket_path = "test-data/sockets/conch-b.sock"

[nodes.NODEA]
root        = "test-data/nodeA"
ip          = "127.0.0.1"
lock_server = "http://127.0.0.1:8080"

[nodes.NODEB]
root        = "test-data/nodeB"
ip          = "127.0.0.1"
lock_server = "http://127.0.0.1:8080"

[nodes.NODEC]
root        = "test-data/nodeC"
ip          = "127.0.0.1"
lock_server = "http://127.0.0.1:8080"
EOF

    cat > test-data/config-nodeC.toml << 'EOF'
current_node = "NODEC"

socket_path = "test-data/sockets/conch-c.sock"

[nodes.NODEA]
root        = "test-data/nodeA"
ip          = "127.0.0.1"
lock_server = "http://127.0.0.1:8080"

[nodes.NODEB]
root        = "test-data/nodeB"
ip          = "127.0.0.1"
lock_server = "http://127.0.0.1:8080"

[nodes.NODEC]
root        = "test-data/nodeC"
ip          = "127.0.0.1"
lock_server = "http://127.0.0.1:8080"
EOF
    echo "✅ 配置文件创建完成"
else
    echo "✅ 配置文件已存在"
fi

# 清理旧的 socket 文件
rm -f test-data/sockets/*.sock

# 启动服务
echo ""
echo "[5/5] 启动服务..."

# 启动 server
echo "  启动 server..."
cd server
./lock-server > ../test-data/logs/server.log 2>&1 &
SERVER_PID=$!
echo $SERVER_PID > ../test-data/logs/server.pid
sleep 2

# 检查 server 是否启动成功
if ! ps -p $SERVER_PID > /dev/null 2>&1; then
    echo "❌ Server 启动失败，查看日志: tail -n 20 test-data/logs/server.log"
    exit 1
fi
echo "  ✅ Server 已启动 (PID: $SERVER_PID)"

# 启动节点 A
echo "  启动节点 A..."
cd ../conchContent-v3
./conchContent -config ../test-data/config-nodeA.toml > ../test-data/logs/nodeA.log 2>&1 &
NODEA_PID=$!
echo $NODEA_PID > ../test-data/logs/nodeA.pid
sleep 1
echo "  ✅ 节点 A 已启动 (PID: $NODEA_PID)"

# 启动节点 B
echo "  启动节点 B..."
./conchContent -config ../test-data/config-nodeB.toml > ../test-data/logs/nodeB.log 2>&1 &
NODEB_PID=$!
echo $NODEB_PID > ../test-data/logs/nodeB.pid
sleep 1
echo "  ✅ 节点 B 已启动 (PID: $NODEB_PID)"

cd ..

echo ""
echo "=========================================="
echo "✅ 所有服务已启动！"
echo "=========================================="
echo ""
echo "服务信息:"
echo "  Server PID:  $SERVER_PID"
echo "  Node A PID:  $NODEA_PID"
echo "  Node B PID:  $NODEB_PID"
echo ""
echo "日志文件:"
echo "  Server:  tail -f test-data/logs/server.log"
echo "  Node A:  tail -f test-data/logs/nodeA.log"
echo "  Node B:  tail -f test-data/logs/nodeB.log"
echo ""
echo "测试命令:"
echo "  # 测试获取锁"
echo "  curl -X POST http://127.0.0.1:8080/lock \\"
echo "    -H 'Content-Type: application/json' \\"
echo "    -d '{\"type\":\"pull\",\"resource_id\":\"sha256:test\",\"node_id\":\"NODEA\"}'"
echo ""
echo "停止服务:"
echo "  ./stop-test.sh"
echo ""

