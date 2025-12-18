# 如何运行测试

## 两个测试文件

### 1. `test-multi-node-multi-layer.go`
- 自己实现HTTP请求和轮询逻辑
- 用于理解锁的分配过程

### 2. `test-client-multi-layer.go` ✅ **推荐**
- 直接使用真实的 `client` 库
- 完全一致，包含所有功能（重试、轮询等）

## 运行方式

### 方式1：分别运行（推荐）

由于两个文件都在同一个目录下，需要分别运行：

```bash
# 运行使用真实client库的测试（推荐）
go run test-client-multi-layer.go

# 或者运行自己实现的测试
go run test-multi-node-multi-layer.go
```

### 方式2：移动到不同目录

```bash
# 创建测试目录
mkdir -p tests/client-library
mkdir -p tests/manual

# 移动文件
mv test-client-multi-layer.go tests/client-library/
mv test-multi-node-multi-layer.go tests/manual/

# 运行
cd tests/client-library && go run test-client-multi-layer.go
cd tests/manual && go run test-multi-node-multi-layer.go
```

## 推荐使用

**推荐使用 `test-client-multi-layer.go`**，因为：

1. ✅ 使用真实的client库，确保测试和实际使用一致
2. ✅ 自动包含重试机制、轮询机制等所有功能
3. ✅ client库更新时，测试自动使用新逻辑
4. ✅ 更接近实际使用场景

## 测试前准备

```bash
# 1. 启动服务器（终端1）
cd server
go run main.go

# 2. 运行测试（终端2）
go run test-client-multi-layer.go
```

