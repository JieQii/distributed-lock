# conchContent-v3 和 server 测试操作指南（Linux）

本文档提供在 Linux 环境下测试 conchContent-v3 和 server 的具体操作命令。

## 前置准备

### 1. 编译 server

```bash
# 在项目根目录
cd server
go build -o lock-server .
```

### 2. 编译 conchContent-v3

```bash
# 在项目根目录
cd conchContent-v3
go build -o conchContent .
```

### 3. 创建测试目录结构

```bash
# 在项目根目录执行
mkdir -p test-data/nodeA/host/blobs/sha256
mkdir -p test-data/nodeA/host/ingest
mkdir -p test-data/nodeA/merged/blobs/sha256
mkdir -p test-data/nodeA/merged/ingest

mkdir -p test-data/nodeB/host/blobs/sha256
mkdir -p test-data/nodeB/host/ingest
mkdir -p test-data/nodeB/merged/blobs/sha256
mkdir -p test-data/nodeB/merged/ingest

mkdir -p test-data/nodeC/host/blobs/sha256
mkdir -p test-data/nodeC/host/ingest
mkdir -p test-data/nodeC/merged/blobs/sha256
mkdir -p test-data/nodeC/merged/ingest

mkdir -p test-data/logs
mkdir -p test-data/sockets
```

### 4. 创建配置文件

#### 节点 A 配置文件 (`test-data/config-nodeA.toml`)

```toml
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
```

#### 节点 B 配置文件 (`test-data/config-nodeB.toml`)

```toml
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
```

#### 节点 C 配置文件 (`test-data/config-nodeC.toml`)

```toml
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
```

## 测试步骤

### 步骤 1: 启动分布式锁 Server

在**第一个终端窗口**中：

```bash
# 在项目根目录
cd server
./lock-server
```

或者后台运行（将输出重定向到日志）：

```bash
cd server
./lock-server > ../test-data/logs/server.log 2>&1 &
echo $! > ../test-data/logs/server.pid
```

**验证 server 是否启动成功：**

```bash
# 检查端口是否监听
netstat -tlnp | grep :8080
# 或使用 ss 命令
ss -tlnp | grep :8080

# 测试 HTTP 接口（如果实现了健康检查）
curl http://127.0.0.1:8080/health

# 查看进程
ps aux | grep lock-server
```

### 步骤 2: 启动 conchContent-v3 节点 A

在**第二个终端窗口**中：

```bash
# 在项目根目录
cd conchContent-v3
./conchContent -config ../test-data/config-nodeA.toml
```

或者后台运行：

```bash
cd conchContent-v3
./conchContent -config ../test-data/config-nodeA.toml > ../test-data/logs/nodeA.log 2>&1 &
echo $! > ../test-data/logs/nodeA.pid
```

### 步骤 3: 启动 conchContent-v3 节点 B

在**第三个终端窗口**中：

```bash
# 在项目根目录
cd conchContent-v3
./conchContent -config ../test-data/config-nodeB.toml
```

或者后台运行：

```bash
cd conchContent-v3
./conchContent -config ../test-data/config-nodeB.toml > ../test-data/logs/nodeB.log 2>&1 &
echo $! > ../test-data/logs/nodeB.pid
```

### 步骤 4: 启动 conchContent-v3 节点 C（可选）

在**第四个终端窗口**中：

```bash
# 在项目根目录
cd conchContent-v3
./conchContent -config ../test-data/config-nodeC.toml
```

或者后台运行：

```bash
cd conchContent-v3
./conchContent -config ../test-data/config-nodeC.toml > ../test-data/logs/nodeC.log 2>&1 &
echo $! > ../test-data/logs/nodeC.pid
```

## 测试场景

### 场景 1: 验证服务启动

**检查 server 状态：**

```bash
# 测试 server 健康检查（如果实现了）
curl http://127.0.0.1:8080/health

# 查看 server 日志
tail -f test-data/logs/server.log

# 或查看最后 20 行
tail -n 20 test-data/logs/server.log
```

**检查 conchContent-v3 状态：**

```bash
# 检查 socket 文件是否创建
ls -l test-data/sockets/

# 检查进程是否运行
ps aux | grep conchContent
```

### 场景 2: 测试分布式锁功能

**使用 curl 测试锁接口：**

```bash
# 测试获取锁（Pull 操作）
curl -X POST http://127.0.0.1:8080/lock \
  -H "Content-Type: application/json" \
  -d '{
    "type": "pull",
    "resource_id": "sha256:test123",
    "node_id": "NODEA"
  }'

# 测试释放锁（成功）
curl -X POST http://127.0.0.1:8080/unlock \
  -H "Content-Type: application/json" \
  -d '{
    "resource_id": "sha256:test123",
    "node_id": "NODEA",
    "success": true
  }'

# 测试释放锁（失败）
curl -X POST http://127.0.0.1:8080/unlock \
  -H "Content-Type: application/json" \
  -d '{
    "resource_id": "sha256:test123",
    "node_id": "NODEA",
    "success": false,
    "error": "下载失败"
  }'
```

### 场景 3: 测试并发锁请求

**使用 bash 脚本并发发送多个锁请求：**

创建 `test-lock.sh` 脚本：

```bash
#!/bin/bash

# test-lock.sh
RESOURCE_ID="sha256:concurrent-test"
SERVER_URL="http://127.0.0.1:8080/lock"

# 并发发送 3 个锁请求
for node in NODEA NODEB NODEC; do
  (
    response=$(curl -s -X POST "$SERVER_URL" \
      -H "Content-Type: application/json" \
      -d "{
        \"type\": \"pull\",
        \"resource_id\": \"$RESOURCE_ID\",
        \"node_id\": \"$node\"
      }")
    echo "[$node] Response: $response"
  ) &
done

# 等待所有后台任务完成
wait
echo "所有请求完成"
```

运行脚本：

```bash
chmod +x test-lock.sh
./test-lock.sh
```

### 场景 4: 测试 Delete 操作（有引用时应该失败）

```bash
# 先让一个节点获取锁并成功
curl -X POST http://127.0.0.1:8080/lock \
  -H "Content-Type: application/json" \
  -d '{
    "type": "pull",
    "resource_id": "sha256:delete-test",
    "node_id": "NODEA"
  }'

# 尝试删除（应该失败，因为有引用）
curl -X POST http://127.0.0.1:8080/lock \
  -H "Content-Type: application/json" \
  -d '{
    "type": "delete",
    "resource_id": "sha256:delete-test",
    "node_id": "NODEB"
  }'

# 释放锁
curl -X POST http://127.0.0.1:8080/unlock \
  -H "Content-Type: application/json" \
  -d '{
    "resource_id": "sha256:delete-test",
    "node_id": "NODEA",
    "success": true
  }'
```

### 场景 5: 查看日志和状态

**查看 server 日志：**

```bash
# 实时查看日志
tail -f test-data/logs/server.log

# 查看最后 50 行
tail -n 50 test-data/logs/server.log

# 搜索特定内容
grep "NODEA" test-data/logs/server.log
```

**查看各节点日志：**

```bash
# 查看节点 A 日志
tail -f test-data/logs/nodeA.log

# 查看所有节点日志
tail -f test-data/logs/*.log
```

**查看进程状态：**

```bash
# 查看所有相关进程
ps aux | grep -E "lock-server|conchContent"

# 查看端口占用
netstat -tlnp | grep -E ":8080|\.sock"
```

## 一键测试脚本

创建 `run-test.sh` 脚本：

```bash
#!/bin/bash
# run-test.sh

set -e

echo "编译 server..."
cd server
go build -o lock-server .
if [ $? -ne 0 ]; then
    echo "编译 server 失败"
    exit 1
fi

echo "编译 conchContent-v3..."
cd ../conchContent-v3
go build -o conchContent .
if [ $? -ne 0 ]; then
    echo "编译 conchContent-v3 失败"
    exit 1
fi

cd ..

# 创建目录
echo "创建测试目录..."
mkdir -p test-data/{nodeA,nodeB,nodeC}/{host,merged}/{blobs/sha256,ingest}
mkdir -p test-data/{logs,sockets}

# 创建配置文件（如果不存在）
if [ ! -f test-data/config-nodeA.toml ]; then
    echo "创建配置文件..."
    # 这里可以添加自动生成配置文件的逻辑
    echo "请手动创建配置文件 test-data/config-nodeA.toml 等"
fi

# 启动 server
echo "启动 server..."
cd server
./lock-server > ../test-data/logs/server.log 2>&1 &
SERVER_PID=$!
echo $SERVER_PID > ../test-data/logs/server.pid
echo "Server PID: $SERVER_PID"

sleep 2

# 启动节点 A
echo "启动节点 A..."
cd ../conchContent-v3
./conchContent -config ../test-data/config-nodeA.toml > ../test-data/logs/nodeA.log 2>&1 &
NODEA_PID=$!
echo $NODEA_PID > ../test-data/logs/nodeA.pid
echo "Node A PID: $NODEA_PID"

sleep 1

# 启动节点 B
echo "启动节点 B..."
./conchContent -config ../test-data/config-nodeB.toml > ../test-data/logs/nodeB.log 2>&1 &
NODEB_PID=$!
echo $NODEB_PID > ../test-data/logs/nodeB.pid
echo "Node B PID: $NODEB_PID"

cd ..

echo ""
echo "所有服务已启动！"
echo "Server PID: $SERVER_PID"
echo "Node A PID: $NODEA_PID"
echo "Node B PID: $NODEB_PID"
echo ""
echo "查看日志:"
echo "  tail -f test-data/logs/server.log"
echo "  tail -f test-data/logs/nodeA.log"
echo "  tail -f test-data/logs/nodeB.log"
echo ""
echo "停止服务: ./stop-test.sh"
```

创建 `stop-test.sh` 脚本：

```bash
#!/bin/bash
# stop-test.sh

echo "停止所有相关进程..."

# 停止 server
if [ -f test-data/logs/server.pid ]; then
    SERVER_PID=$(cat test-data/logs/server.pid)
    if ps -p $SERVER_PID > /dev/null 2>&1; then
        kill $SERVER_PID
        echo "已停止 server (PID: $SERVER_PID)"
    fi
fi

# 停止各节点
for node in nodeA nodeB nodeC; do
    if [ -f test-data/logs/$node.pid ]; then
        NODE_PID=$(cat test-data/logs/$node.pid)
        if ps -p $NODE_PID > /dev/null 2>&1; then
            kill $NODE_PID
            echo "已停止 $node (PID: $NODE_PID)"
        fi
    fi
done

# 强制清理残留进程
pkill -f lock-server 2>/dev/null || true
pkill -f conchContent 2>/dev/null || true

# 清理 socket 文件
rm -f test-data/sockets/*.sock

echo "清理完成"
```

使用脚本：

```bash
# 给脚本添加执行权限
chmod +x run-test.sh stop-test.sh

# 运行测试
./run-test.sh

# 停止所有服务
./stop-test.sh
```

## 清理脚本

创建 `clean-test.sh` 脚本：

```bash
#!/bin/bash
# clean-test.sh

echo "停止所有相关进程..."

# 停止所有相关进程
pkill -f lock-server 2>/dev/null || true
pkill -f conchContent 2>/dev/null || true

# 清理测试数据（可选）
read -p "是否删除测试数据？(y/n) " -n 1 -r
echo
if [[ $REPLY =~ ^[Yy]$ ]]; then
    rm -rf test-data
    echo "测试数据已清理"
fi

# 清理编译产物（可选）
read -p "是否删除编译产物？(y/n) " -n 1 -r
echo
if [[ $REPLY =~ ^[Yy]$ ]]; then
    rm -f server/lock-server
    rm -f conchContent-v3/conchContent
    echo "编译产物已清理"
fi

echo "清理完成"
```

## 常见问题排查

### 1. Server 无法启动

```bash
# 检查端口是否被占用
netstat -tlnp | grep :8080
# 或
lsof -i :8080

# 如果被占用，可以修改 server/main.go 中的端口，或设置环境变量
export PORT=8081
cd server
./lock-server
```

### 2. conchContent-v3 无法连接 server

```bash
# 检查 server 是否运行
curl http://127.0.0.1:8080/health

# 检查配置文件中的 lock_server 地址是否正确
cat test-data/config-nodeA.toml | grep lock_server

# 检查网络连接
telnet 127.0.0.1 8080
```

### 3. Socket 文件创建失败

```bash
# 检查 socket 目录权限
ls -ld test-data/sockets

# 检查是否有旧 socket 文件残留
rm -f test-data/sockets/*.sock

# 检查目录是否存在
mkdir -p test-data/sockets
```

### 4. mergerfs 挂载失败

```bash
# 检查 mergerfs 是否安装
which mergerfs

# 如果没有安装，可以安装（Ubuntu/Debian）
sudo apt-get install mergerfs

# 或者（CentOS/RHEL）
sudo yum install mergerfs

# 检查挂载点
mount | grep mergerfs

# 卸载旧的挂载（如果需要）
fusermount -u test-data/nodeA/merged/blobs/sha256
```

### 5. 查看详细错误信息

```bash
# 查看 server 详细日志
tail -n 100 test-data/logs/server.log

# 查看 conchContent 输出
tail -n 100 test-data/logs/nodeA.log

# 查看系统日志（如果有）
journalctl -u your-service-name -n 50
```

### 6. 权限问题

```bash
# 如果遇到权限问题，检查文件权限
ls -la test-data/

# 修改权限（如果需要）
chmod -R 755 test-data/
```

## 使用 tmux 或 screen 管理多个终端

### 使用 tmux

```bash
# 安装 tmux（如果没有）
sudo apt-get install tmux  # Ubuntu/Debian
# 或
sudo yum install tmux      # CentOS/RHEL

# 创建新会话
tmux new -s test

# 在 tmux 中分割窗口
# Ctrl+b, %  # 垂直分割
# Ctrl+b, "  # 水平分割

# 在不同窗格中运行不同的服务
# 窗格 1: ./lock-server
# 窗格 2: ./conchContent -config ../test-data/config-nodeA.toml
# 窗格 3: ./conchContent -config ../test-data/config-nodeB.toml

# 分离会话: Ctrl+b, d
# 重新连接: tmux attach -t test
```

### 使用 screen

```bash
# 安装 screen（如果没有）
sudo apt-get install screen

# 创建新会话
screen -S test

# 在 screen 中创建新窗口: Ctrl+a, c
# 切换窗口: Ctrl+a, n (下一个) 或 Ctrl+a, p (上一个)

# 分离会话: Ctrl+a, d
# 重新连接: screen -r test
```

## 注意事项

1. **mergerfs 依赖**: `conchContent-v3/config.go` 中使用了 `mergerfs` 命令，需要先安装 mergerfs
   ```bash
   sudo apt-get install mergerfs  # Ubuntu/Debian
   ```

2. **路径**: 配置文件中的路径可以使用相对路径或绝对路径

3. **后台运行**: 使用 `&` 后台运行进程时，建议保存 PID 以便后续管理

4. **端口冲突**: 确保 8080 端口未被其他程序占用

5. **日志管理**: 建议定期清理日志文件，避免占用过多磁盘空间

6. **测试环境隔离**: 建议在独立的测试目录中运行，避免影响生产环境
