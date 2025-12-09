# 测试运行指南

本文档说明如何运行分布式锁系统的测试用例。

## 前置要求

1. **Go环境**：确保已安装Go 1.21或更高版本
   ```bash
   go version
   ```

2. **安装依赖**：
   ```bash
   go mod download
   ```

## 运行测试

### 1. 运行所有测试

在项目根目录执行：

```bash
go test ./...
```

这会运行所有子包中的测试（server、client等）。

### 2. 运行特定包的测试

#### 运行服务端测试

```bash
# 进入server目录
cd server
go test

# 或者从根目录运行
go test ./server
```

#### 运行客户端测试

```bash
# 进入client目录
cd client
go test

# 或者从根目录运行
go test ./client
```

### 3. 运行特定测试用例

#### 使用 `-run` 参数

```bash
# 运行包含"Pull"的测试
go test -run Pull ./server

# 运行包含"Concurrent"的测试
go test -run Concurrent ./server

# 运行特定测试函数
go test -run TestConcurrentPullOperations ./server
go test -run TestDeleteWithReferences ./server
go test -run TestRetryMechanism ./client
```

### 4. 查看详细输出

#### 使用 `-v` 参数（verbose）

```bash
# 显示所有测试的详细输出
go test -v ./server
go test -v ./client

# 运行所有测试并显示详细输出
go test -v ./...
```

#### 使用 `-run` 和 `-v` 组合

```bash
# 运行特定测试并显示详细输出
go test -v -run TestConcurrentPullOperations ./server
```

### 5. 运行测试并查看覆盖率

```bash
# 生成覆盖率报告
go test -cover ./server
go test -cover ./client

# 生成详细的覆盖率报告
go test -coverprofile=coverage.out ./server
go test -tool cover -html=coverage.out

# 查看所有包的覆盖率
go test -cover ./...
```

### 6. 运行测试并显示测试时间

```bash
go test -v -timeout 30s ./server
```

### 7. 运行基准测试（如果有）

```bash
go test -bench=. ./server
go test -bench=. -benchmem ./server
```

## 测试用例说明

### 服务端测试 (`server/lock_manager_test.go`)

#### 1. TestConcurrentPullOperations
- **功能**：测试并发pull操作的引用计数准确性
- **运行**：`go test -v -run TestConcurrentPullOperations ./server`
- **说明**：10个节点并发执行pull操作，验证引用计数是否正确

#### 2. TestPullSkipWhenRefCountNotZero
- **功能**：测试Pull操作在refcount != 0时跳过
- **运行**：`go test -v -run TestPullSkipWhenRefCountNotZero ./server`
- **说明**：验证当资源已下载完成时，后续节点会跳过操作

#### 3. TestDeleteWithReferences
- **功能**：测试有引用时删除操作
- **运行**：`go test -v -run TestDeleteWithReferences ./server`
- **说明**：验证有节点使用资源时，delete操作会失败

#### 4. TestDeleteWithoutReferences
- **功能**：测试无引用时删除操作
- **运行**：`go test -v -run TestDeleteWithoutReferences ./server`
- **说明**：验证没有节点使用资源时，delete操作可以成功

#### 5. TestDeleteWhenRefCountZero
- **功能**：测试Delete操作在refcount == 0时的情况
- **运行**：`go test -v -run TestDeleteWhenRefCountZero ./server`
- **说明**：验证refcount == 0时允许执行delete操作

#### 6. TestUpdateWithReferences
- **功能**：测试有引用时update操作（默认允许热更新）
- **运行**：`go test -v -run TestUpdateWithReferences ./server`
- **说明**：验证默认配置下允许在有引用时更新

#### 7. TestUpdateWithoutReferencesRequired
- **功能**：测试配置要求无引用时update操作
- **运行**：`go test -v -run TestUpdateWithoutReferencesRequired ./server`
- **说明**：验证配置为不允许热更新时的行为

#### 8. TestFIFOQueue
- **功能**：测试FIFO队列顺序
- **运行**：`go test -v -run TestFIFOQueue ./server`
- **说明**：验证请求按先进先出顺序获得锁

#### 9. TestConcurrentDifferentResources
- **功能**：测试不同资源的并发操作
- **运行**：`go test -v -run TestConcurrentDifferentResources ./server`
- **说明**：验证不同资源可以并发操作，互不干扰

#### 10. TestReferenceCountAccuracy
- **功能**：测试引用计数准确性
- **运行**：`go test -v -run TestReferenceCountAccuracy ./server`
- **说明**：验证引用计数和节点集合的准确性

### 客户端测试 (`client/client_test.go`)

#### 1. TestConcurrentPullOperations
- **功能**：测试客户端并发pull操作
- **运行**：`go test -v -run TestConcurrentPullOperations ./client`
- **说明**：测试多个客户端并发请求锁

#### 2. TestDeleteWithReferences
- **功能**：测试有引用时删除操作（客户端）
- **运行**：`go test -v -run TestDeleteWithReferences ./client`
- **说明**：验证客户端正确处理delete操作的错误响应

#### 3. TestRetryMechanism
- **功能**：测试重试机制
- **运行**：`go test -v -run TestRetryMechanism ./client`
- **说明**：验证客户端在网络错误时自动重试

#### 4. TestTimeout
- **功能**：测试超时处理
- **运行**：`go test -v -run TestTimeout ./client`
- **说明**：验证客户端正确处理请求超时

## 常用测试命令组合

### 快速验证所有测试

```bash
# 运行所有测试，显示详细输出
go test -v ./...

# 运行所有测试，显示覆盖率
go test -cover ./...
```

### 调试特定测试

```bash
# 运行特定测试，显示详细输出和测试时间
go test -v -timeout 60s -run TestConcurrentPullOperations ./server

# 运行测试并生成覆盖率报告
go test -v -coverprofile=server_coverage.out -run TestConcurrentPullOperations ./server
go tool cover -html=server_coverage.out
```

### 并发测试

```bash
# 运行所有并发相关测试
go test -v -run Concurrent ./server

# 运行引用计数相关测试
go test -v -run RefCount ./server
```

## 测试输出示例

### 成功运行的输出

```
=== RUN   TestConcurrentPullOperations
    lock_manager_test.go:72: 引用计数正确: 10 个节点正在使用资源
    lock_manager_test.go:73: 成功执行: 10, 跳过操作: 0
--- PASS: TestConcurrentPullOperations (0.12s)
PASS
ok      distributed-lock/server    0.234s
```

### 失败的输出

```
=== RUN   TestDeleteWithReferences
    lock_manager_test.go:105: 正确返回错误: 无法删除：当前有节点正在使用该资源
--- PASS: TestDeleteWithReferences (0.01s)
PASS
```

## 故障排查

### 1. 测试超时

如果测试运行时间过长，可以增加超时时间：

```bash
go test -timeout 5m ./server
```

### 2. 测试失败

如果测试失败，查看详细输出：

```bash
go test -v -run TestName ./server
```

### 3. 依赖问题

如果遇到依赖问题，更新依赖：

```bash
go mod tidy
go mod download
```

### 4. 清理测试缓存

如果测试结果异常，清理测试缓存：

```bash
go clean -testcache
go test ./...
```

## 持续集成（CI）示例

### GitHub Actions 示例

```yaml
name: Tests

on: [push, pull_request]

jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v2
      - uses: actions/setup-go@v2
        with:
          go-version: '1.21'
      - run: go mod download
      - run: go test -v -cover ./...
```

## 性能测试

### 运行基准测试（如果添加了基准测试）

```bash
# 运行所有基准测试
go test -bench=. ./server

# 运行基准测试并显示内存分配
go test -bench=. -benchmem ./server

# 运行特定基准测试
go test -bench=BenchmarkLock ./server
```

## 测试最佳实践

1. **运行测试前**：确保所有依赖已安装
   ```bash
   go mod tidy
   ```

2. **开发时**：频繁运行相关测试
   ```bash
   go test -v -run TestName ./server
   ```

3. **提交前**：运行所有测试
   ```bash
   go test -v ./...
   ```

4. **查看覆盖率**：定期检查测试覆盖率
   ```bash
   go test -cover ./...
   ```

## 快速参考

| 命令 | 说明 |
|------|------|
| `go test ./...` | 运行所有测试 |
| `go test -v ./server` | 运行server包测试，显示详细输出 |
| `go test -run TestName ./server` | 运行特定测试 |
| `go test -cover ./server` | 显示测试覆盖率 |
| `go test -timeout 60s ./server` | 设置测试超时时间 |
| `go clean -testcache` | 清理测试缓存 |

## 注意事项

1. **并发测试**：某些测试涉及并发操作，可能需要较长时间
2. **网络测试**：客户端测试需要模拟HTTP服务器，使用httptest
3. **资源清理**：测试会自动清理资源，无需手动清理
4. **测试隔离**：每个测试都是独立的，不会相互影响

