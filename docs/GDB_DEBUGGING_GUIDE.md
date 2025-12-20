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

### 1. 设置关键断点

```bash
# 在订阅者注册处设置断点
(gdb) break lock_manager.go:275
(gdb) break lock_manager.go:323

# 在事件广播处设置断点
(gdb) break lock_manager.go:329

# 在 SSE 发送事件处设置断点
(gdb) break sse_subscriber.go:35

# 在操作完成处设置断点
(gdb) break lock_manager.go:160
```

### 2. 调试订阅流程

```bash
# 启动程序
(gdb) run

# 程序会在断点处停止，查看变量
(gdb) p lockType
(gdb) p resourceID
(gdb) p subscriber

# 查看订阅者列表
(gdb) p shard.subscribers[key]

# 继续执行
(gdb) continue
```

### 3. 调试事件广播

```bash
# 当程序停在 broadcastEvent 时
(gdb) p event
(gdb) p event.Type
(gdb) p event.ResourceID
(gdb) p event.Success

# 查看订阅者数量
(gdb) p len(subscribers)

# 单步执行，观察事件发送过程
(gdb) next
(gdb) next
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

## 实际调试示例

### 示例1：调试订阅者注册

```bash
# 1. 启动 GDB
gdb ./lock-server-debug

# 2. 设置断点
(gdb) break lock_manager.go:275
(gdb) break handler.go:148

# 3. 运行程序
(gdb) run

# 4. 在另一个终端发送订阅请求
curl "http://localhost:8086/lock/subscribe?type=pull&resource_id=sha256:test"

# 5. 程序会在断点处停止，查看变量
(gdb) p lockType
(gdb) p resourceID
(gdb) p subscriber

# 6. 继续执行
(gdb) continue
```

### 示例2：调试事件广播

```bash
# 1. 设置断点
(gdb) break lock_manager.go:160
(gdb) break lock_manager.go:329

# 2. 运行程序
(gdb) run

# 3. 在另一个终端执行操作
curl -X POST http://localhost:8086/lock \
  -H "Content-Type: application/json" \
  -d '{"type":"pull","resource_id":"sha256:test","node_id":"node-1"}'

curl -X POST http://localhost:8086/unlock \
  -H "Content-Type: application/json" \
  -d '{"type":"pull","resource_id":"sha256:test","node_id":"node-1","success":true}'

# 4. 程序会在断点处停止
(gdb) p event
(gdb) p len(subscribers)

# 5. 单步执行，观察广播过程
(gdb) next
(gdb) next
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

