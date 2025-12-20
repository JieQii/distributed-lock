# 测试阶段说明

## 测试分层

测试分为三个阶段，**不需要真实的 containerd 下载镜像**，直到最后一个阶段：

---

## 阶段1：客户端库测试（当前阶段）✅

### 测试内容
- **只测试客户端库本身**（`client/` 目录）
- 测试锁的获取、释放、队列等待、轮询等功能
- **不涉及 containerd**
- **不涉及真实的镜像下载**

### 测试方式
```bash
# 1. 启动服务器
cd server
./lock-server

# 2. 运行客户端测试
cd client
go test -v

# 3. 运行集成测试脚本
./test-client-basic.sh      # 基本功能
./test-client-queue.sh       # 队列等待
./test-client-polling.sh     # 轮询机制
```

### 测试范围
- ✅ 锁的获取和释放
- ✅ 队列等待机制
- ✅ 轮询机制（发现操作已完成）
- ✅ 重试机制
- ✅ 超时处理
- ✅ 错误处理

### 不需要
- ❌ containerd
- ❌ 真实的镜像下载
- ❌ 文件系统操作
- ❌ mergerfs

---

## 阶段2：conchContent-v3 集成测试（下一阶段）

### 测试内容
- **测试 conchContent-v3 的完整流程**
- 测试引用计数管理
- 测试 Writer 的封装逻辑
- **仍然不需要真实的 containerd**
- **可以模拟 containerd 的调用**

### 测试方式
```bash
# 1. 启动服务器
cd server
./lock-server

# 2. 启动 conchContent-v3（作为 gRPC 服务）
cd conchContent-v3
./conchContent -config ../test-data/config-nodeA.toml

# 3. 使用模拟的 containerd 客户端测试
# （编写测试代码模拟 containerd 调用 conchContent-v3 的 gRPC 接口）
```

### 测试范围
- ✅ 引用计数检查（ShouldSkipOperation）
- ✅ Writer 的创建和使用
- ✅ 锁的集成（OpenWriter → Lock → Unlock）
- ✅ 引用计数的更新
- ✅ gRPC 接口的调用

### 不需要
- ❌ 真实的 containerd 进程
- ❌ 真实的镜像下载
- ✅ 可以模拟 containerd 的 gRPC 调用

---

## 阶段3：完整端到端测试（最后阶段）

### 测试内容
- **使用真实的 containerd**
- **真实的镜像下载**
- **多节点场景**
- **完整的文件系统操作**

### 测试方式
```bash
# 1. 启动服务器
cd server
./lock-server

# 2. 启动多个 containerd 实例（模拟多节点）
# 3. 启动多个 conchContent-v3 实例
# 4. 使用真实的 containerd 客户端下载镜像
ctr --address /run/containerd-a/containerd.sock images pull docker.io/library/busybox:latest
```

### 测试范围
- ✅ 真实的 containerd 集成
- ✅ 真实的镜像下载
- ✅ 多节点并发下载
- ✅ 文件系统操作（host/merged）
- ✅ mergerfs 挂载
- ✅ 引用计数的持久化

### 需要
- ✅ 真实的 containerd
- ✅ 真实的镜像仓库访问
- ✅ 文件系统操作
- ✅ mergerfs

---

## 当前阶段：客户端库测试

### 你现在应该做什么

**✅ 已完成**：
- 服务器端测试（锁管理功能）
- 基础功能测试（获取/释放锁）

**🔄 当前进行**：
- 客户端库测试（`client/` 目录）
- 测试锁的获取、释放、队列等待、轮询等功能

**📋 测试清单**：
- [ ] 运行单元测试：`cd client && go test -v`
- [ ] 测试基本功能：`./test-client-basic.sh`
- [ ] 测试队列等待：`./test-client-queue.sh`
- [ ] 测试轮询机制：`./test-client-polling.sh`
- [ ] 验证所有功能正常

### 不需要做的事情

- ❌ **不需要启动 containerd**
- ❌ **不需要下载真实的镜像**
- ❌ **不需要配置 containerd**
- ❌ **不需要文件系统操作**

---

## 测试顺序建议

### 第一步：客户端库测试（当前）

```bash
# 1. 启动服务器
cd server && ./lock-server &

# 2. 测试客户端库
cd client
go test -v
./test-client-basic.sh
./test-client-queue.sh
./test-client-polling.sh
```

**目标**：验证客户端库的所有功能正常

### 第二步：conchContent-v3 集成测试（下一步）

```bash
# 1. 启动服务器
cd server && ./lock-server &

# 2. 启动 conchContent-v3
cd conchContent-v3
./conchContent -config ../test-data/config-nodeA.toml &

# 3. 编写测试代码模拟 containerd 调用
# （使用 gRPC 客户端调用 conchContent-v3 的接口）
```

**目标**：验证 conchContent-v3 的集成逻辑正常

### 第三步：完整端到端测试（最后）

```bash
# 1. 启动服务器
# 2. 启动多个 containerd 实例
# 3. 启动多个 conchContent-v3 实例
# 4. 使用真实的 containerd 下载镜像
```

**目标**：验证完整的端到端流程正常

---

## 总结

### 你的理解 ✅ 完全正确

1. **当前阶段（客户端库测试）**：
   - ✅ 不需要真实的 containerd
   - ✅ 不需要真实的镜像下载
   - ✅ 只测试锁的获取和释放功能

2. **下一阶段（conchContent-v3 集成测试）**：
   - ✅ 可以模拟 containerd 的调用
   - ✅ 不需要真实的 containerd 进程
   - ✅ 测试引用计数和 Writer 逻辑

3. **最后阶段（完整端到端测试）**：
   - ✅ 使用真实的 containerd
   - ✅ 真实的镜像下载
   - ✅ 完整的文件系统操作

### 当前应该做什么

**专注于客户端库测试**：
- 运行单元测试
- 运行集成测试脚本
- 验证锁的获取、释放、队列等待、轮询等功能
- **不需要考虑 containerd 或镜像下载**

完成客户端库测试后，再进入下一阶段的 conchContent-v3 集成测试。

