# 快速开始测试指南

## 快速命令参考

### 1. 一键启动测试环境

```bash
# 给脚本添加执行权限（首次使用）
chmod +x run-test.sh stop-test.sh test-lock.sh test-concurrent.sh

# 启动所有服务
./run-test.sh
```

### 2. 测试分布式锁功能

```bash
# 基础锁测试
./test-lock.sh

# 并发锁测试
./test-concurrent.sh
```

### 3. 手动测试命令

```bash
# 获取锁
curl -X POST http://127.0.0.1:8080/lock \
  -H "Content-Type: application/json" \
  -d '{"type":"pull","resource_id":"sha256:test","node_id":"NODEA"}'

# 释放锁
curl -X POST http://127.0.0.1:8080/unlock \
  -H "Content-Type: application/json" \
  -d '{"resource_id":"sha256:test","node_id":"NODEA","success":true}'
```

### 4. 查看日志

```bash
# 实时查看 server 日志
tail -f test-data/logs/server.log

# 查看节点 A 日志
tail -f test-data/logs/nodeA.log

# 查看所有日志
tail -f test-data/logs/*.log
```

### 5. 停止所有服务

```bash
./stop-test.sh
```

## 详细文档

更多详细信息请参考：
- `TESTING_COMMANDS.md` - 完整的测试操作指南
- `TESTING_GUIDE.md` - 单元测试指南
- `TESTING_SCENARIO_CONTENT_PLUGIN.md` - Content 插件测试场景

## 常见问题

### 端口被占用

```bash
# 检查端口占用
netstat -tlnp | grep :8080

# 或使用 ss
ss -tlnp | grep :8080

# 修改端口（设置环境变量）
export PORT=8081
cd server
./lock-server
```

### mergerfs 未安装

```bash
# Ubuntu/Debian
sudo apt-get install mergerfs

# CentOS/RHEL
sudo yum install mergerfs
```

### 查看进程状态

```bash
# 查看所有相关进程
ps aux | grep -E "lock-server|conchContent"

# 查看 PID 文件
cat test-data/logs/*.pid
```

