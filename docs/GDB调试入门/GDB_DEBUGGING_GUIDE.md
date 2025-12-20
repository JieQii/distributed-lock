# GDB 调试服务端操作指南

## 概述

本文档介绍如何使用 GDB (GNU Debugger) 调试 Go 语言编写的分布式锁服务端程序。

---

## 前置准备

### 1. 安装 GDB

**Windows:**
```bash
# 使用 MinGW 或 MSYS2 安装
# 或者下载预编译版本: https://sourceforge.net/projects/mingw-w64/files/

# 验证安装
gdb --version
```

**Linux:**
```bash
sudo apt-get install gdb
# 或
sudo yum install gdb
```

**macOS:**
```bash
brew install gdb
# 注意：macOS 可能需要代码签名，参考后续说明
```

### 2. 编译支持调试的二进制文件

Go 程序需要编译时包含调试信息：

```bash
cd server

# 编译带调试信息的二进制文件
go build -gcflags="all=-N -l" -o lock-server-debug main.go handler.go lock_manager.go types.go sse_subscriber.go

# 参数说明：
# -gcflags="all=-N -l": 
#   -N: 禁用优化
#   -l: 禁用内联
#   这些选项确保调试时能看到完整的代码和变量
```

---

## 基本调试流程

### 1. 启动 GDB

```bash
# 方式1：直接启动并加载程序
gdb ./lock-server-debug

# 方式2：先启动 GDB，再加载程序
gdb
(gdb) file lock-server-debug
```

### 2. 设置断点

```bash
# 在 main 函数设置断点
(gdb) break main.main
# 或简写
(gdb) b main.main

# 在特定文件的行号设置断点
(gdb) break lock_manager.go:63
(gdb) b lock_manager.go:63

# 在函数名设置断点
(gdb) break (*LockManager).TryLock
(gdb) b (*LockManager).Unlock
(gdb) b (*Handler).Lock
(gdb) b (*Handler).Subscribe

# 查看所有断点
(gdb) info breakpoints
# 或简写
(gdb) i b

# 删除断点
(gdb) delete 1
# 或
(gdb) d 1
```

### 3. 运行程序

```bash
# 运行程序（会停在第一个断点）
(gdb) run
# 或简写
(gdb) r

# 带参数运行
(gdb) run --port 8086

# 设置环境变量
(gdb) set environment PORT=8086
(gdb) run
```

### 4. 基本调试命令

```bash
# 继续执行（到下一个断点）
(gdb) continue
# 或简写
(gdb) c

# 单步执行（进入函数）
(gdb) step
# 或简写
(gdb) s

# 单步执行（不进入函数，逐行执行）
(gdb) next
# 或简写
(gdb) n

# 执行到当前函数返回
(gdb) finish
# 或简写
(gdb) fin

# 查看当前代码上下文
(gdb) list
# 或简写
(gdb) l

# 查看变量值
(gdb) print variable_name
# 或简写
(gdb) p variable_name

# 查看变量类型
(gdb) ptype variable_name

# 查看结构体所有字段
(gdb) print *variable_name

# 查看局部变量
(gdb) info locals
# 或简写
(gdb) i locals

# 查看函数参数
(gdb) info args
# 或简写
(gdb) i args

# 查看调用栈
(gdb) backtrace
# 或简写
(gdb) bt

# 查看当前帧信息
(gdb) info frame
# 或简写
(gdb) i f
```

---

## 调试订阅者模式

### 重要提示：如何触发断点

GDB 断点只有在代码执行到该位置时才会触发。对于 HTTP 服务器：
1. 程序启动后，会一直运行等待请求
2. **需要从另一个终端发送 HTTP 请求来触发断点**
3. 当请求到达时，程序会停在断点处

### 完整调试流程示例

#### 步骤1：启动 GDB 并设置断点

```bash
# 编译带调试信息的程序
cd server
go build -gcflags="all=-N -l" -o lock-server-debug main.go handler.go lock_manager.go types.go sse_subscriber.go

# 启动 GDB
gdb ./lock-server-debug

# 在 GDB 中设置断点
(gdb) break lock_manager.go:275
(gdb) break handler.go:148
(gdb) break lock_manager.go:160
(gdb) break lock_manager.go:329

# 查看所有断点
(gdb) info breakpoints
```

#### 步骤2：运行程序

```bash
# 在 GDB 中运行程序
(gdb) run

# 程序会启动并等待请求，此时不会停在断点
# 你会看到类似输出：
# 2025/12/20 16:29:20 锁服务端启动在端口 8080
```

**重要**：程序现在在等待 HTTP 请求，断点还没有被触发！

#### 步骤3：在另一个终端发送请求触发断点

打开**新的终端窗口**，发送订阅请求：

```bash
# 发送订阅请求（这会触发 handler.go:148 和 lock_manager.go:275 的断点）
curl -N "http://localhost:8086/lock/subscribe?type=pull&resource_id=sha256:test123"
```

#### 步骤4：当断点触发时查看变量

当请求到达时，GDB 会自动停在断点处。此时你可以查看变量：

```bash
# 查看当前代码位置
(gdb) list

# 查看函数参数（在 Subscribe 函数中）
(gdb) info args
# 或
(gdb) p lockType
(gdb) p resourceID
(gdb) p subscriber

# 查看局部变量
(gdb) info locals

# 查看调用栈
(gdb) backtrace
# 或简写
(gdb) bt

# 查看当前帧的详细信息
(gdb) info frame
```

#### 步骤5：继续执行并触发下一个断点

```bash
# 继续执行（会触发下一个断点或完成当前请求）
(gdb) continue
# 或简写
(gdb) c
```

#### 步骤6：调试操作完成事件

在另一个终端发送操作请求：

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

当程序停在 `lock_manager.go:160`（Unlock 函数）时：

```bash
# 查看请求参数
(gdb) p request
(gdb) p request.Type
(gdb) p request.ResourceID
(gdb) p request.NodeID
(gdb) p request.Success

# 查看锁信息
(gdb) p lockInfo
(gdb) p lockInfo.Completed
(gdb) p lockInfo.Success

# 单步执行
(gdb) next
```

当程序停在 `lock_manager.go:329`（broadcastEvent 函数）时：

```bash
# 查看事件
(gdb) p event
(gdb) p event.Type
(gdb) p event.ResourceID
(gdb) p event.Success

# 查看订阅者列表
(gdb) p subscribers
(gdb) p len(subscribers)

# 查看订阅者详情
(gdb) p subscribers[0]
```

### 常见问题：为什么看不到变量？

**问题**：`No symbol "lockType" in current context`

**原因**：
1. 程序当前不在该变量的作用域内
2. 可能停在系统调用或其他 goroutine 中
3. 需要先让程序执行到断点处

**解决方法**：

```bash
# 1. 确认程序是否停在断点处
(gdb) where
# 或
(gdb) bt

# 2. 如果不在断点处，继续执行
(gdb) continue

# 3. 如果停在系统调用中，需要等待 HTTP 请求触发断点
# 在另一个终端发送请求

# 4. 查看当前帧
(gdb) frame
(gdb) info frame

# 5. 切换到正确的帧（如果调用栈很深）
(gdb) frame 2
(gdb) up 2
(gdb) down 1
```

### 调试技巧：使用条件断点

```bash
# 只在特定条件下触发断点
(gdb) break lock_manager.go:160 if request.Success == true
(gdb) break lock_manager.go:329 if len(subscribers) > 0

# 查看断点条件
(gdb) info breakpoints
```

---

## 高级调试技巧

### 1. 条件断点

```bash
# 只在特定条件下触发断点
(gdb) break lock_manager.go:160 if request.Success == true
(gdb) break lock_manager.go:160 if request.NodeID == "node-1"

# 查看断点条件
(gdb) info breakpoints
```

### 2. 监视变量

```bash
# 监视变量变化
(gdb) watch variable_name

# 监视表达式
(gdb) watch (variable.field == value)

# 查看所有监视点
(gdb) info watchpoints
```

### 3. 修改变量值

```bash
# 修改变量值（用于测试）
(gdb) set variable request.Success = true
(gdb) set variable nodeID = "test-node"
```

### 4. 调用函数

```bash
# 在调试过程中调用函数
(gdb) call function_name(arguments)

# 例如：调用日志函数
(gdb) call log.Printf("Debug: %s", variable)
```

### 5. 多线程调试

```bash
# 查看所有线程
(gdb) info threads
# 或简写
(gdb) i threads

# 切换到指定线程
(gdb) thread 2
# 或简写
(gdb) t 2

# 所有线程执行相同命令
(gdb) thread apply all bt
```

---

## 调试 HTTP 请求处理

### 1. 设置 HTTP 处理断点

```bash
# 在 Lock 处理函数设置断点
(gdb) break handler.go:24

# 在 Unlock 处理函数设置断点
(gdb) break handler.go:74

# 在 Subscribe 处理函数设置断点
(gdb) break handler.go:148
```

### 2. 查看 HTTP 请求信息

```bash
# 当程序停在 Handler 函数时
(gdb) p request
(gdb) p request.Type
(gdb) p request.ResourceID
(gdb) p request.NodeID

# 查看 HTTP 响应
(gdb) p response
```

---

## 实际调试示例（完整流程）

### 示例1：调试订阅者注册（完整步骤）

**终端1（GDB 调试）**：

```bash
# 1. 编译并启动 GDB
cd server
go build -gcflags="all=-N -l" -o lock-server-debug main.go handler.go lock_manager.go types.go sse_subscriber.go
gdb ./lock-server-debug

# 2. 设置断点
(gdb) break handler.go:148
Breakpoint 1 at 0x7f1234: file handler.go, line 148
(gdb) break lock_manager.go:275
Breakpoint 2 at 0x7f0ab6: file lock_manager.go, line 275

# 3. 运行程序
(gdb) run
Starting program: /path/to/lock-server-debug
[New Thread ...]
2025/12/20 16:29:20 锁服务端启动在端口 8086
# 程序现在在等待请求，不会停在断点
```

**终端2（发送请求）**：

```bash
# 发送订阅请求，这会触发断点
curl -N "http://localhost:8086/lock/subscribe?type=pull&resource_id=sha256:test123"
# 注意：curl 会挂起等待 SSE 事件
```

**回到终端1（GDB）**：

```bash
# 程序现在停在 handler.go:148（Subscribe 函数）
Breakpoint 1, main.(*Handler).Subscribe (w=..., r=0xc0001a2000) at handler.go:148
148     func (h *Handler) Subscribe(w http.ResponseWriter, r *http.Request) {

# 查看变量
(gdb) list
(gdb) p typeParam
(gdb) p resourceIDParam

# 继续执行，会停在 lock_manager.go:275
(gdb) continue
Breakpoint 2, main.(*LockManager).Subscribe (lm=0xc0000a0000, lockType=..., resourceID=..., subscriber=...) at lock_manager.go:275
275     func (lm *LockManager) Subscribe(lockType, resourceID string, subscriber Subscriber) string {

# 现在可以查看参数
(gdb) p lockType
$1 = "pull"
(gdb) p resourceID
$2 = "sha256:test123"
(gdb) p subscriber

# 单步执行
(gdb) next
(gdb) next

# 查看订阅者列表
(gdb) p shard.subscribers[key]

# 继续执行，让请求完成
(gdb) continue
```

### 示例2：调试事件广播（完整步骤）

**终端1（GDB 调试）**：

```bash
# 1. 设置断点
(gdb) break lock_manager.go:160
Breakpoint 3 at 0x7ef8c5: file lock_manager.go, line 160
(gdb) break lock_manager.go:329
Breakpoint 4 at 0x7f19c5: file lock_manager.go, line 329

# 2. 运行程序（如果还没运行）
(gdb) run
```

**终端2（执行操作）**：

```bash
# 先获取锁
curl -X POST http://localhost:8086/lock \
  -H "Content-Type: application/json" \
  -d '{"type":"pull","resource_id":"sha256:test123","node_id":"node-1"}'

# 然后释放锁（这会触发断点）
curl -X POST http://localhost:8086/unlock \
  -H "Content-Type: application/json" \
  -d '{"type":"pull","resource_id":"sha256:test123","node_id":"node-1","success":true}'
```

**回到终端1（GDB）**：

```bash
# 程序停在 lock_manager.go:160（Unlock 函数中）
Breakpoint 3, main.(*LockManager).Unlock (lm=0xc0000a0000, request=0xc0001b4000) at lock_manager.go:160
160         lm.broadcastEvent(shard, key, &OperationEvent{

# 查看请求信息
(gdb) p request
$1 = (main.UnlockRequest *) 0xc0001b4000
(gdb) p *request
$2 = {Type = "pull", ResourceID = "sha256:test123", NodeID = "node-1", Success = true, Error = ""}

# 查看锁信息
(gdb) p lockInfo
(gdb) p lockInfo.Completed
(gdb) p lockInfo.Success

# 单步执行，会进入 broadcastEvent
(gdb) next
Breakpoint 4, main.(*LockManager).broadcastEvent (lm=0xc0000a0000, shard=0xc0000a0100, key=..., event=0xc0001b5000) at lock_manager.go:329
329         log.Printf("[BroadcastEvent] 广播事件: key=%s, 订阅者数量=%d, success=%v",

# 查看事件
(gdb) p *event
$3 = {Type = "pull", ResourceID = "sha256:test123", NodeID = "node-1", Success = true, Error = "", CompletedAt = {...}}

# 查看订阅者
(gdb) p subscribers
(gdb) p len(subscribers)
$4 = 1

# 继续执行，观察事件发送
(gdb) next
(gdb) next
```

### 调试技巧：使用 GDB 命令脚本

创建 `debug_subscriber.gdb` 文件：

```bash
# 设置断点
break handler.go:148
break lock_manager.go:275
break lock_manager.go:160
break lock_manager.go:329

# 运行程序
run

# 定义命令：显示订阅相关信息
define show_subscribe
    printf "=== Subscribe Debug ===\n"
    printf "lockType: %s\n", lockType
    printf "resourceID: %s\n", resourceID
    printf "subscriber: %p\n", subscriber
    bt
end

# 定义命令：显示事件信息
define show_event
    printf "=== Event Debug ===\n"
    printf "Event Type: %s\n", event->Type
    printf "ResourceID: %s\n", event->ResourceID
    printf "Success: %d\n", event->Success
    printf "Subscribers: %d\n", len(subscribers)
    bt
end

# 在断点处自动执行命令
commands 2
    show_subscribe
    continue
end

commands 4
    show_event
    continue
end
```

使用脚本：

```bash
gdb -x debug_subscriber.gdb ./lock-server-debug
```

---

## GDB 配置文件 (.gdbinit)

创建 `~/.gdbinit` 文件，添加常用配置：

```bash
# 显示结构体时展开所有字段
set print pretty on

# 显示数组时显示所有元素
set print elements 0

# 显示字符串时显示完整内容
set print null-stop on

# 自动反汇编
set disassembly-flavor intel

# 定义常用命令别名
define gostack
    bt
    info goroutines
end
document gostack
    显示 Go 调用栈和 goroutine 信息
end

define govars
    info locals
    info args
end
document govars
    显示当前函数的局部变量和参数
end
```

---

## 常见问题排查

### 1. 无法设置断点

**问题**: `No symbol table is loaded`

**解决**:
```bash
# 确保编译时包含调试信息
go build -gcflags="all=-N -l" -o lock-server-debug

# 在 GDB 中重新加载符号表
(gdb) file lock-server-debug
```

### 2. 断点位置不准确

**问题**: Go 编译器优化导致行号不匹配

**解决**:
```bash
# 使用 -N -l 禁用优化和内联
go build -gcflags="all=-N -l" -o lock-server-debug
```

### 3. macOS 代码签名问题

**问题**: `Unable to find Mach task port`

**解决**:
```bash
# 创建代码签名证书（需要管理员权限）
# 1. 打开 Keychain Access
# 2. 创建自签名证书
# 3. 签名 GDB
codesign -fs gdb-cert /usr/local/bin/gdb

# 或者使用 lldb（macOS 默认调试器）
lldb ./lock-server-debug
```

### 4. 无法查看 Go 变量

**问题**: 变量显示为 `<optimized out>`

**解决**:
```bash
# 确保编译时禁用优化
go build -gcflags="all=-N -l" -o lock-server-debug

# 如果仍然有问题，尝试查看寄存器
(gdb) info registers
```

---

## 调试技巧总结

1. **使用日志配合调试**: 在关键位置添加日志，配合 GDB 断点使用
2. **分步调试**: 先设置断点验证基本流程，再深入细节
3. **使用条件断点**: 减少不必要的停止，提高调试效率
4. **保存调试会话**: 使用 GDB 脚本自动化常见调试流程
5. **结合单元测试**: 在测试中重现问题，再用 GDB 调试

---

## 参考资源

- [GDB 官方文档](https://sourceware.org/gdb/documentation/)
- [Go 调试指南](https://golang.org/doc/diagnostics.html)
- [Delve 调试器](https://github.com/go-delve/delve) - Go 专用调试器（推荐用于 Go 项目）

---

## 快速参考卡片

```bash
# 启动和运行
gdb ./lock-server-debug
(gdb) run
(gdb) continue

# 断点
(gdb) break file.go:line
(gdb) break function_name
(gdb) info breakpoints

# 执行控制
(gdb) step          # 进入函数
(gdb) next          # 下一行
(gdb) finish        # 执行到返回

# 查看信息
(gdb) print var
(gdb) backtrace
(gdb) info locals
(gdb) info args

# 线程
(gdb) info threads
(gdb) thread N
```

