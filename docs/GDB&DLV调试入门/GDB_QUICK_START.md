# GDB 调试快速入门

## 为什么看不到变量？

当你看到 `No symbol "lockType" in current context` 时，通常是因为：

1. **程序还没有执行到断点处** - HTTP 服务器启动后一直在等待请求
2. **程序停在系统调用中** - 不在用户代码的上下文
3. **需要发送 HTTP 请求来触发断点**

## 正确的调试步骤

### 第一步：启动 GDB 并设置断点

```bash
cd server
go build -gcflags="all=-N -l" -o lock-server-debug main.go handler.go lock_manager.go types.go sse_subscriber.go
gdb ./lock-server-debug

# 设置断点
(gdb) break handler.go:148
(gdb) break lock_manager.go:275
(gdb) break lock_manager.go:160
(gdb) break lock_manager.go:329

# 运行程序
(gdb) run
```

**重要**：程序启动后会显示日志，然后**一直等待**，不会停在断点。这是正常的！

### 第二步：在另一个终端发送请求

打开**新的终端窗口**（不要关闭 GDB），发送 HTTP 请求：

```bash
# 这会触发 handler.go:148 和 lock_manager.go:275 的断点
curl -N "http://localhost:8086/lock/subscribe?type=pull&resource_id=sha256:test123"
```

### 第三步：回到 GDB 查看变量

当请求到达时，GDB 会自动停在断点处：

```bash
# 查看当前代码位置
(gdb) list

# 查看函数参数
(gdb) info args
# 或直接查看变量（现在可以看到了！）
(gdb) p lockType
$1 = "pull"
(gdb) p resourceID
$2 = "sha256:test123"

# 查看调用栈
(gdb) backtrace

# 继续执行
(gdb) continue
```

## 调试事件广播

### 步骤1：确保订阅者已连接

在终端2中保持订阅请求运行（curl 会一直等待）。

### 步骤2：发送操作请求

在**第三个终端**中：

```bash
# 获取锁
curl -X POST http://localhost:8086/lock \
  -H "Content-Type: application/json" \
  -d '{"type":"pull","resource_id":"sha256:test123","node_id":"node-1"}'

# 释放锁（这会触发 lock_manager.go:160 和 lock_manager.go:329 的断点）
curl -X POST http://localhost:8086/unlock \
  -H "Content-Type: application/json" \
  -d '{"type":"pull","resource_id":"sha256:test123","node_id":"node-1","success":true}'
```

### 步骤3：在 GDB 中查看

```bash
# 程序停在 lock_manager.go:160
(gdb) p request
(gdb) p request.Success
(gdb) p lockInfo

# 继续执行，会停在 lock_manager.go:329
(gdb) continue

# 现在可以查看事件和订阅者
(gdb) p event
(gdb) p event.Type
(gdb) p len(subscribers)
```

## 常见错误和解决方法

### 错误1：程序停在系统调用中

```
Thread 1 "lock-server-deb" received signal SIGINT, Interrupt.
runtime/internal/syscall.Syscall6 () at /usr/lib/golang/src/runtime/internal/syscall/asm_linux_amd64.s:36
```

**解决方法**：
```bash
# 继续执行，让程序回到等待状态
(gdb) continue

# 然后在另一个终端发送请求来触发断点
```

### 错误2：看不到变量

```
(gdb) p lockType
No symbol "lockType" in current context.
```

**解决方法**：
1. 确认程序是否停在断点处：`(gdb) where`
2. 如果不在断点处，发送 HTTP 请求触发断点
3. 如果停在系统调用中，使用 `continue` 继续执行

### 错误3：断点没有触发

**可能原因**：
1. 请求没有发送到服务器
2. 断点位置不正确
3. 代码路径没有执行到断点

**解决方法**：
```bash
# 检查断点是否设置成功
(gdb) info breakpoints

# 检查程序是否在运行
(gdb) info program

# 尝试在更早的位置设置断点（如 main 函数）
(gdb) break main.main
(gdb) run
```

## 实用 GDB 命令速查

```bash
# 基本命令
(gdb) run              # 运行程序
(gdb) continue         # 继续执行
(gdb) next             # 下一行（不进入函数）
(gdb) step             # 下一行（进入函数）
(gdb) finish           # 执行到函数返回

# 查看信息
(gdb) list             # 显示代码
(gdb) backtrace        # 查看调用栈
(gdb) info args        # 查看函数参数
(gdb) info locals      # 查看局部变量
(gdb) info breakpoints # 查看所有断点

# 查看变量
(gdb) print variable   # 打印变量
(gdb) p variable       # 简写
(gdb) p *pointer       # 打印指针指向的内容

# 断点管理
(gdb) break file.go:line    # 设置断点
(gdb) delete N              # 删除断点
(gdb) disable N             # 禁用断点
(gdb) enable N              # 启用断点

# 线程管理
(gdb) info threads          # 查看所有线程
(gdb) thread N              # 切换到线程 N
```

## 完整调试示例

```bash
# 终端1：GDB
gdb ./lock-server-debug
(gdb) break handler.go:148
(gdb) break lock_manager.go:160
(gdb) run
# 等待...

# 终端2：订阅
curl -N "http://localhost:8086/lock/subscribe?type=pull&resource_id=test"

# 终端1：GDB 停在 handler.go:148
(gdb) p typeParam
(gdb) continue

# 终端3：操作
curl -X POST http://localhost:8086/unlock \
  -H "Content-Type: application/json" \
  -d '{"type":"pull","resource_id":"test","node_id":"node-1","success":true}'

# 终端1：GDB 停在 lock_manager.go:160
(gdb) p request
(gdb) p event
(gdb) continue
```

## 提示

1. **保持多个终端打开** - GDB、订阅请求、操作请求
2. **使用 `continue` 而不是 `run`** - 程序已经在运行
3. **查看调用栈** - `backtrace` 帮助你理解代码执行路径
4. **使用 `list` 查看代码** - 确认当前执行位置
5. **耐心等待** - HTTP 服务器需要请求才会触发断点

