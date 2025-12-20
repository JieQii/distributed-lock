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
- **测试场景**：
  - 10个节点（A-J）并发尝试对同一资源（sha256:test123）执行pull操作
  - 每个节点获取锁后模拟10ms的操作时间，然后释放锁并标记成功
- **预期结果**：
  - 只有一个节点能够成功获取锁并执行操作（其他节点会跳过或等待）
  - 成功执行的节点数 = 引用计数
  - 引用计数应该等于实际成功执行pull操作的节点数（通常为1）
  - 其他节点会看到引用计数 > 0，从而跳过操作
- **验证点**：
  - 引用计数准确性：`refCount.Count == successCount`
  - 并发安全性：多个节点同时操作不会导致引用计数错误

#### 2. TestPullSkipWhenRefCountNotZero
- **功能**：测试Pull操作在refcount != 0时跳过
- **运行**：`go test -v -run TestPullSkipWhenRefCountNotZero ./server`
- **测试场景**：
  - 节点1先执行pull操作，获取锁后释放并标记成功（引用计数变为1）
  - 节点2随后尝试对同一资源执行pull操作
- **预期结果**：
  - 节点1：成功获取锁，执行操作，引用计数变为1
  - 节点2：尝试获取锁时，发现引用计数 > 0，应该跳过操作（skip = true, acquired = false）
  - 引用计数保持为1（只有节点1在使用资源）
- **验证点**：
  - 引用计数正确更新：`refCount.Count == 1`
  - 后续节点正确跳过：`skip2 == true && acquired2 == false`
  - 无错误信息返回

#### 3. TestDeleteWithReferences
- **功能**：测试有引用时删除操作
- **运行**：`go test -v -run TestDeleteWithReferences ./server`
- **测试场景**：
  - 节点1先执行pull操作，成功完成后引用计数变为1
  - 节点2尝试对同一资源执行delete操作
- **预期结果**：
  - 节点1：成功pull，引用计数 = 1
  - 节点2：尝试delete时，系统检测到引用计数 > 0
  - 返回错误：`"无法删除：当前有节点正在使用该资源"`
  - `acquired = false, skip = false, errMsg != ""`
- **验证点**：
  - 引用计数检查正确：`refCount.Count == 1`
  - delete操作被拒绝：`acquired == false && skip == false`
  - 错误信息正确返回

#### 4. TestDeleteWithoutReferences
- **功能**：测试无引用时删除操作
- **运行**：`go test -v -run TestDeleteWithoutReferences ./server`
- **测试场景**：
  - 资源初始状态：无任何节点使用（引用计数 = 0）
  - 节点1尝试对资源执行delete操作
- **预期结果**：
  - 节点1：成功获取delete锁（因为引用计数 = 0）
  - 执行delete操作，标记成功
  - delete成功后，引用计数被清理（删除资源）
  - 最终引用计数 = 0
- **验证点**：
  - delete操作成功：`acquired == true && skip == false && errMsg == ""`
  - 引用计数被清理：`refCount.Count == 0`

#### 5. TestDeleteWhenRefCountZero
- **功能**：测试Delete操作在refcount == 0时的情况
- **运行**：`go test -v -run TestDeleteWhenRefCountZero ./server`
- **测试场景**：
  - 资源从未被pull过，引用计数初始为0
  - 节点1尝试执行delete操作（资源可能不存在）
- **预期结果**：
  - 节点1：可以获取delete锁（refcount == 0允许delete）
  - 执行delete操作，标记成功
  - 引用计数被清理
- **验证点**：
  - delete操作允许执行：`acquired == true && skip == false && errMsg == ""`
  - 系统允许在refcount == 0时执行delete（处理资源不存在的情况）

#### 6. TestUpdateWithReferences
- **功能**：测试有引用时update操作（默认允许热更新）
- **运行**：`go test -v -run TestUpdateWithReferences ./server`
- **测试场景**：
  - 配置：`UpdateRequiresNoRef = false`（允许热更新）
  - 节点1先执行pull操作，成功完成后引用计数变为1
  - 节点2尝试对同一资源执行update操作
- **预期结果**：
  - 节点1：成功pull，引用计数 = 1
  - 节点2：可以获取update锁（允许热更新）
  - `acquired = true, skip = false, errMsg == ""`
- **验证点**：
  - 热更新功能正常：即使引用计数 > 0，update操作也能成功
  - 引用计数不受update操作影响（仍为1）

#### 7. TestUpdateWithoutReferencesRequired
- **功能**：测试配置要求无引用时update操作
- **运行**：`go test -v -run TestUpdateWithoutReferencesRequired ./server`
- **测试场景**：
  - 配置：`UpdateRequiresNoRef = true`（不允许热更新）
  - 节点1先执行pull操作，成功完成后引用计数变为1
  - 节点2尝试对同一资源执行update操作
- **预期结果**：
  - 节点1：成功pull，引用计数 = 1
  - 节点2：尝试update时，系统检测到配置要求无引用且引用计数 > 0
  - 返回错误：`"无法更新：当前有节点正在使用该资源，不允许更新"`
  - `acquired = false, skip = false, errMsg != ""`
- **验证点**：
  - 配置检查正确：当`UpdateRequiresNoRef = true`时，有引用时update被拒绝
  - 错误信息正确返回

#### 8. TestFIFOQueue
- **功能**：测试FIFO队列顺序
- **运行**：`go test -v -run TestFIFOQueue ./server`
- **测试场景**：
  - 节点1先获取pull锁（持有锁）
  - 节点2尝试获取pull锁（进入等待队列）
  - 节点3尝试获取pull锁（进入等待队列）
  - 节点1释放锁，标记操作失败（Success = false）
- **预期结果**：
  - 节点1：成功获取锁
  - 节点2：无法立即获取锁，进入队列（队列长度 = 1）
  - 节点3：无法立即获取锁，进入队列（队列长度 = 2）
  - 节点1释放锁（失败）：锁立即释放，队列中的下一个节点（节点2）获得锁
  - 最终：节点2持有锁，队列中只剩下节点3（队列长度 = 1）
- **验证点**：
  - FIFO顺序：节点2先于节点3获得锁
  - 队列长度正确：`queueLen == 1`（节点3在队列中）
  - 锁持有者正确：`lockInfo.Request.NodeID == "node-2"`

#### 9. TestConcurrentDifferentResources
- **功能**：测试不同资源的并发操作
- **运行**：`go test -v -run TestConcurrentDifferentResources ./server`
- **测试场景**：
  - 5个节点（A-E）并发操作资源1（sha256:resource1）
  - 5个节点（F-J）并发操作资源2（sha256:resource2）
  - 每个节点获取锁后模拟10ms操作，然后释放锁
- **预期结果**：
  - 资源1：只有一个节点成功获取锁并执行操作（其他节点会跳过，因为引用计数 > 0）
  - 资源2：只有一个节点成功获取锁并执行操作
  - 总共成功执行的操作数 = 2（每个资源1个）
  - 不同资源之间互不干扰，可以并发操作
- **验证点**：
  - 资源隔离：不同资源的操作互不影响
  - 同一资源只有一个节点成功：`successCount == 2`（每个资源1个）
  - 并发安全性：多个资源同时操作不会产生冲突

#### 10. TestReferenceCountAccuracy
- **功能**：测试引用计数准确性
- **运行**：`go test -v -run TestReferenceCountAccuracy ./server`
- **测试场景**：
  - 节点1执行pull操作，成功完成后引用计数变为1
  - 节点2尝试执行pull操作
- **预期结果**：
  - 节点1：成功获取锁，执行操作，引用计数 = 1，节点集合包含"node-1"
  - 节点2：尝试获取锁时，发现引用计数 > 0，跳过操作（skip = true）
  - 引用计数保持为1，节点集合只包含"node-1"
- **验证点**：
  - 引用计数准确性：`refCount.Count == 1`
  - 节点集合准确性：`refCount.Nodes["node-1"] == true`
  - 后续节点正确跳过：`skip2 == true && acquired2 == false`

### 客户端测试 (`client/client_test.go`)

#### 1. TestConcurrentPullOperations
- **功能**：测试客户端并发pull操作
- **运行**：`go test -v -run TestConcurrentPullOperations ./client`
- **测试场景**：
  - 创建模拟HTTP服务器，总是返回成功获取锁的响应
  - 10个客户端节点并发请求同一资源的pull锁
  - 每个节点获取锁后模拟10ms操作，然后释放锁
- **预期结果**：
  - 所有10个节点都能成功获取锁（因为模拟服务器总是返回成功）
  - 所有节点都能成功释放锁
  - 成功获取锁的节点数 = 10
- **验证点**：
  - 客户端并发请求处理正确
  - HTTP请求和响应处理正确
  - 锁的获取和释放流程正常

#### 2. TestDeleteWithReferences
- **功能**：测试有引用时删除操作（客户端）
- **运行**：`go test -v -run TestDeleteWithReferences ./client`
- **测试场景**：
  - 创建模拟HTTP服务器，返回403状态码和错误信息
  - 客户端尝试执行delete操作
  - 服务器返回：`{"acquired":false,"skip":false,"error":"无法删除：当前有节点正在使用该资源"}`
- **预期结果**：
  - 客户端成功发送delete请求
  - 客户端正确解析错误响应
  - `result.Error != nil`，包含错误信息
  - `result.Acquired == false`
- **验证点**：
  - 错误响应解析正确：能够识别403状态码和错误信息
  - 错误信息正确传递：`result.Error`包含服务器返回的错误

#### 3. TestRetryMechanism
- **功能**：测试重试机制
- **运行**：`go test -v -run TestRetryMechanism ./client`
- **测试场景**：
  - 创建模拟HTTP服务器，前两次请求返回503错误（Service Unavailable）
  - 第三次请求返回成功响应
  - 客户端配置：`MaxRetries = 3, RetryInterval = 100ms`
- **预期结果**：
  - 第1次请求：返回503错误，客户端重试
  - 第2次请求：返回503错误，客户端重试
  - 第3次请求：返回成功，客户端获得锁
  - 总共尝试3次（初始请求 + 2次重试）
  - `result.Acquired == true`
- **验证点**：
  - 重试机制正确：在服务器错误时自动重试
  - 重试次数正确：`attemptCount == 3`
  - 最终成功获取锁

#### 4. TestTimeout
- **功能**：测试超时处理
- **运行**：`go test -v -run TestTimeout ./client`
- **测试场景**：
  - 创建模拟HTTP服务器，延迟2秒响应
  - 客户端配置：`RequestTimeout = 500ms`
  - 客户端尝试获取锁
- **预期结果**：
  - 客户端发送请求
  - 服务器延迟2秒响应
  - 客户端在500ms后超时
  - 返回超时错误：`context deadline exceeded`
  - `err != nil`，包含超时信息
- **验证点**：
  - 超时机制正确：在指定时间后取消请求
  - 超时错误正确返回：错误信息包含超时相关信息
  - 不会无限等待

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

