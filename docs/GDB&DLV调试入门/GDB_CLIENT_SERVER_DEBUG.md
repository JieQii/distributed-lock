# GDB 调试客户端和服务端完整指南（已过时，推荐使用 Delve）

⚠️ **注意**：此文档使用 GDB 调试 Go 程序。**强烈推荐使用 Delve (dlv)**，它是 Go 专用的调试器，对 Go 语言的支持更好。

**推荐使用**：请查看 `docs/DLV_DEBUGGING_GUIDE.md` 获取 Delve 调试指南。

## 目录
1. [服务端调试](#服务端调试)
2. [客户端调试](#客户端调试)
3. [同时调试客户端和服务端](#同时调试客户端和服务端)

---

## 服务端调试

### 步骤1：编译带调试信息的服务端

```bash
cd server
go build -gcflags="all=-N -l" -o lock-server-debug main.go handler.go lock_manager.go types.go sse_subscriber.go
```

**参数说明**：
- `-gcflags="all=-N -l"`: 
  - `-N`: 禁用优化
  - `-l`: 禁用内联
  - 确保调试时能看到完整的代码和变量

### 步骤2：启动 GDB 并设置断点

```bash
gdb ./lock-server-debug
```

**在 GDB 中设置断点**：

```bash
# 在 main 函数设置断点
(gdb) break main.main
# 或
(gdb) b main.main

# 在关键函数设置断点
(gdb) break handler.go:41          # Lock 处理函数
(gdb) break handler.go:69           # Unlock 处理函数
(gdb) break handler.go:148          # Subscribe 处理函数
(gdb) break lock_manager.go:67      # TryLock 函数
(gdb) break lock_manager.go:128     # Unlock 函数
(gdb) break lock_manager.go:329     # broadcastEvent 函数

# 查看所有断点
(gdb) info breakpoints
```

### 步骤3：运行服务端

```bash
# 在 GDB 中运行
(gdb) run

# 或带环境变量
(gdb) set environment PORT=8086
(gdb) run
```

**重要**：程序启动后会显示日志，然后**一直等待 HTTP 请求**，不会停在断点。这是正常的！

你会看到类似输出：
```
2025/12/20 16:29:20 锁服务端启动在端口 8086
```

### 步骤4：在另一个终端发送请求触发断点

打开**新的终端窗口**，发送 HTTP 请求：

```bash
# 发送加锁请求（触发 handler.go:41 和 lock_manager.go:67）
curl -X POST http://localhost:8086/lock \
  -H "Content-Type: application/json" \
  -d '{"type":"pull","resource_id":"sha256:test123","node_id":"node-1"}'

# 发送解锁请求（触发 handler.go:69 和 lock_manager.go:128）
curl -X POST http://localhost:8086/unlock \
  -H "Content-Type: application/json" \
  -d '{"type":"pull","resource_id":"sha256:test123","node_id":"node-1","error":""}'

# 发送订阅请求（触发 handler.go:148）
curl -N "http://localhost:8086/lock/subscribe?type=pull&resource_id=sha256:test123"
```

### 步骤5：在 GDB 中查看变量

当请求到达时，GDB 会自动停在断点处：

```bash
# 查看当前代码位置
(gdb) list

# 查看函数参数
(gdb) info args
# 或直接查看变量
(gdb) p request
(gdb) p request.Type
(gdb) p request.ResourceID
(gdb) p request.NodeID

# 查看局部变量
(gdb) info locals

# 查看调用栈
(gdb) backtrace
# 或简写
(gdb) bt

# 单步执行
(gdb) next        # 下一行（不进入函数）
(gdb) step        # 下一行（进入函数）
(gdb) continue    # 继续执行到下一个断点
```

---

## 客户端调试

### 步骤1：编译带调试信息的客户端测试程序

```bash
# 编译测试程序
go build -gcflags="all=-N -l" -o test-client-debug test-client-multi-layer.go

# 或编译 contentv2（如果调试 contentv2）
cd contentv2
go build -gcflags="all=-N -l" -o contentv2-debug main.go store.go writer.go config.go
```

### 步骤2：启动 GDB 并设置断点

```bash
gdb ./test-client-debug
```

**在 GDB 中设置断点**：

```bash
# 在 main 函数设置断点
(gdb) break main.main

# 在客户端关键函数设置断点
(gdb) break client/client.go:74      # tryLockOnce
(gdb) break client/client.go:146      # waitForLock
(gdb) break client/client.go:260      # handleOperationEvent
(gdb) break client/client.go:434      # tryUnlockOnce

# 在测试程序关键位置设置断点
(gdb) break test-client-multi-layer.go:39   # Lock 调用后
(gdb) break test-client-multi-layer.go:55   # 获得锁后
(gdb) break test-client-multi-layer.go:66   # 释放锁前

# 查看所有断点
(gdb) info breakpoints
```

### 步骤3：运行客户端（需要先启动服务端）

**先确保服务端在运行**（在另一个终端或后台）：

```bash
# 终端1：启动服务端（非调试模式）
cd server
go run main.go handler.go lock_manager.go types.go sse_subscriber.go
# 或
./lock-server-debug
```

**然后在 GDB 中运行客户端**：

```bash
# 在 GDB 中运行客户端
(gdb) run

# 或带参数（如果是 contentv2）
(gdb) run --config config.toml
```

### 步骤4：调试客户端代码

当程序停在断点时：

```bash
# 查看当前代码位置
(gdb) list

# 查看变量
(gdb) p request
(gdb) p request.Type
(gdb) p request.ResourceID
(gdb) p request.NodeID
(gdb) p result
(gdb) p result.Acquired
(gdb) p result.Error

# 查看调用栈
(gdb) backtrace

# 单步执行
(gdb) next
(gdb) step
(gdb) continue
```

### 步骤5：调试 SSE 订阅流程

如果调试 `waitForLock` 函数（SSE 订阅）：

```bash
# 设置断点
(gdb) break client/client.go:146      # waitForLock 入口
(gdb) break client/client.go:260      # handleOperationEvent

# 运行程序
(gdb) run

# 程序会停在 waitForLock，此时客户端正在等待 SSE 事件
# 在另一个终端发送操作请求，触发服务端广播事件
```

---

## 同时调试客户端和服务端

### 方法1：使用两个 GDB 实例（推荐）

**终端1：调试服务端**

```bash
cd server
go build -gcflags="all=-N -l" -o lock-server-debug main.go handler.go lock_manager.go types.go sse_subscriber.go
gdb ./lock-server-debug

# 设置断点
(gdb) break handler.go:41
(gdb) break lock_manager.go:128
(gdb) break lock_manager.go:329

# 运行
(gdb) run
```

**终端2：调试客户端**

```bash
# 编译客户端
go build -gcflags="all=-N -l" -o test-client-debug test-client-multi-layer.go

# 启动 GDB
gdb ./test-client-debug

# 设置断点
(gdb) break client/client.go:74
(gdb) break client/client.go:146
(gdb) break client/client.go:260

# 运行
(gdb) run
```

**调试流程**：

1. 两个 GDB 都运行后，客户端会发送请求到服务端
2. 服务端 GDB 会停在 `handler.go:41`（Lock 处理）
3. 在服务端 GDB 中查看请求，然后 `continue`
4. 客户端 GDB 会停在 `client/client.go:74`（tryLockOnce）
5. 在客户端 GDB 中查看响应，然后 `continue`
6. 继续这个过程，观察客户端和服务端的交互

### 方法2：使用 GDB 的远程调试

**服务端（被调试程序）**：

```bash
# 使用 gdbserver（Linux）
gdbserver :2345 ./lock-server-debug

# 或使用 GDB 的远程调试功能
gdb ./lock-server-debug
(gdb) target remote localhost:2345
```

**客户端（调试器）**：

```bash
gdb ./test-client-debug
# 正常调试
```

---

## 常用调试场景

### 场景1：调试锁获取流程

**服务端断点**：
```bash
(gdb) break lock_manager.go:67      # TryLock
(gdb) break lock_manager.go:76      # 检查锁是否存在
(gdb) break lock_manager.go:116     # 创建新锁
```

**客户端断点**：
```bash
(gdb) break client/client.go:74     # tryLockOnce
(gdb) break client/client.go:134    # 处理响应
```

**调试步骤**：
1. 客户端发送请求 → 服务端停在 `TryLock`
2. 查看服务端锁状态 → `continue`
3. 客户端收到响应 → 客户端停在 `tryLockOnce`
4. 查看客户端结果 → `continue`

### 场景2：调试 SSE 订阅和事件广播

**服务端断点**：
```bash
(gdb) break handler.go:148          # Subscribe
(gdb) break lock_manager.go:275     # Subscribe（LockManager）
(gdb) break lock_manager.go:128     # Unlock
(gdb) break lock_manager.go:329     # broadcastEvent
```

**客户端断点**：
```bash
(gdb) break client/client.go:146    # waitForLock
(gdb) break client/client.go:260    # handleOperationEvent
```

**调试步骤**：
1. 客户端订阅 → 服务端停在 `Subscribe`
2. 查看订阅者注册 → `continue`
3. 客户端等待事件 → 客户端停在 `waitForLock`
4. 服务端解锁 → 服务端停在 `Unlock`
5. 服务端广播事件 → 服务端停在 `broadcastEvent`
6. 客户端收到事件 → 客户端停在 `handleOperationEvent`
7. 查看事件内容 → `continue`

### 场景3：调试错误处理

**设置条件断点**：
```bash
# 只在有错误时触发
(gdb) break lock_manager.go:67 if errMsg != ""
(gdb) break client/client.go:125 if lockResp.Error != ""
```

---

## 实用 GDB 命令速查

```bash
# 基本命令
(gdb) run              # 运行程序
(gdb) continue         # 继续执行（简写：c）
(gdb) next             # 下一行，不进入函数（简写：n）
(gdb) step             # 下一行，进入函数（简写：s）
(gdb) finish           # 执行到函数返回（简写：fin）

# 查看信息
(gdb) list             # 显示代码（简写：l）
(gdb) backtrace         # 查看调用栈（简写：bt）
(gdb) info args         # 查看函数参数（简写：i args）
(gdb) info locals       # 查看局部变量（简写：i locals）
(gdb) info breakpoints  # 查看所有断点（简写：i b）

# 查看变量
(gdb) print variable    # 打印变量（简写：p）
(gdb) p *pointer       # 打印指针指向的内容
(gdb) ptype variable   # 查看变量类型

# 断点管理
(gdb) break file.go:line           # 设置断点（简写：b）
(gdb) break function_name          # 在函数设置断点
(gdb) break file.go:line if cond  # 条件断点
(gdb) delete N                     # 删除断点（简写：d）
(gdb) disable N                    # 禁用断点
(gdb) enable N                     # 启用断点

# 线程管理（Go 程序）
(gdb) info threads      # 查看所有线程（简写：i threads）
(gdb) thread N         # 切换到线程 N（简写：t N）

# 修改变量（用于测试）
(gdb) set variable var = value

# 调用函数
(gdb) call function_name(args)
```

---

## 常见问题

### 问题1：看不到变量

**原因**：程序还没有执行到断点处，或停在系统调用中

**解决**：
```bash
# 确认程序是否停在断点处
(gdb) where
(gdb) backtrace

# 如果停在系统调用中，继续执行
(gdb) continue

# 然后发送请求触发断点
```

### 问题2：断点没有触发

**原因**：代码路径没有执行到断点

**解决**：
```bash
# 检查断点是否设置成功
(gdb) info breakpoints

# 在更早的位置设置断点（如 main 函数）
(gdb) break main.main
(gdb) run
```

### 问题3：Go 变量显示异常

**原因**：Go 的运行时特性导致 GDB 难以解析变量

**解决**：
```bash
# 确保编译时禁用优化
go build -gcflags="all=-N -l" -o program-debug

# 使用 Delve 调试器（Go 专用，推荐）
dlv debug ./program
```

---

## 推荐：使用 Delve（Go 专用调试器）

Delve 是 Go 专用的调试器，比 GDB 更适合 Go 程序：

```bash
# 安装 Delve
go install github.com/go-delve/delve/cmd/dlv@latest

# 调试服务端
cd server
dlv debug

# 调试客户端
dlv debug test-client-multi-layer.go

# Delve 命令（类似 GDB）
(dlv) break main.main
(dlv) break client/client.go:74
(dlv) continue
(dlv) next
(dlv) print variable
(dlv) args
(dlv) locals
```

---

## 完整调试示例

### 示例：调试完整的锁获取和释放流程

**终端1：服务端 GDB**
```bash
cd server
go build -gcflags="all=-N -l" -o lock-server-debug main.go handler.go lock_manager.go types.go sse_subscriber.go
gdb ./lock-server-debug

(gdb) break handler.go:41
(gdb) break lock_manager.go:67
(gdb) break lock_manager.go:128
(gdb) run
```

**终端2：客户端 GDB**
```bash
go build -gcflags="all=-N -l" -o test-client-debug test-client-multi-layer.go
gdb ./test-client-debug

(gdb) break client/client.go:74
(gdb) break client/client.go:434
(gdb) run
```

**终端3：观察日志**
```bash
# 可以在这里查看日志输出
tail -f server.log
```

**调试流程**：
1. 客户端发送加锁请求 → 服务端停在 `handler.go:41`
2. 在服务端查看请求 → `continue` → 停在 `lock_manager.go:67`
3. 在服务端查看锁状态 → `continue`
4. 客户端收到响应 → 客户端停在 `client/client.go:74`
5. 在客户端查看结果 → `continue`
6. 客户端发送解锁请求 → 服务端停在 `lock_manager.go:128`
7. 在服务端查看解锁逻辑 → `continue`
8. 客户端收到解锁响应 → 客户端停在 `client/client.go:434`
9. 在客户端查看解锁结果 → `continue`

---

## 总结

1. **编译时添加调试信息**：`-gcflags="all=-N -l"`
2. **设置断点**：在关键函数和位置设置断点
3. **运行程序**：使用 `run` 启动程序
4. **触发断点**：通过 HTTP 请求或程序执行触发断点
5. **查看变量**：使用 `print` 和 `info` 命令查看变量和调用栈
6. **单步执行**：使用 `next`、`step`、`continue` 控制执行流程

**提示**：对于 Go 程序，推荐使用 Delve 调试器，它比 GDB 更适合 Go 的特性。

