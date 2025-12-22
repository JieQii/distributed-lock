# Delve (dlv) 调试客户端和服务端完整指南

Delve 是 Go 专用的调试器，比 GDB 更适合 Go 程序，支持 goroutine、channel、interface 等 Go 特性。

## 目录
1. [安装 Delve](#安装-delve)
2. [服务端调试](#服务端调试)
3. [客户端调试](#客户端调试)
4. [同时调试客户端和服务端](#同时调试客户端和服务端)
5. [常用调试场景](#常用调试场景)

---

## 安装 Delve

```bash
# 安装 Delve
go install github.com/go-delve/delve/cmd/dlv@latest

# 验证安装
dlv version

# 如果 dlv 命令找不到，确保 $GOPATH/bin 或 $HOME/go/bin 在 PATH 中
export PATH=$PATH:$(go env GOPATH)/bin
```

---

## 服务端调试

### 方法1：直接调试（推荐）

```bash
cd server
dlv debug
```

这会自动编译并启动调试器。

### 方法2：调试已编译的二进制文件

```bash
cd server
go build -o lock-server-debug
dlv exec ./lock-server-debug
```

### 设置断点并运行

```bash
# 启动 Delve
cd server
dlv debug

# 在 Delve 中设置断点
(dlv) break main.main
(dlv) break handler.go:41          # Lock 处理函数
(dlv) break handler.go:69          # Unlock 处理函数
(dlv) break handler.go:148         # Subscribe 处理函数
(dlv) break lock_manager.go:67     # TryLock 函数
(dlv) break lock_manager.go:128    # Unlock 函数
(dlv) break lock_manager.go:329    # broadcastEvent 函数

# 查看所有断点
(dlv) breakpoints

# 运行程序
(dlv) continue
# 或简写
(dlv) c
```

**重要**：程序启动后会显示日志，然后**一直等待 HTTP 请求**，不会停在断点。这是正常的！

你会看到类似输出：
```
2025/12/20 16:29:20 锁服务端启动在端口 8086
```

### 在另一个终端发送请求触发断点

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

### 在 Delve 中查看变量

当请求到达时，Delve 会自动停在断点处：

```bash
# 查看当前代码位置
(dlv) list

# 查看函数参数
(dlv) args
# 或直接查看变量
(dlv) print request
(dlv) print request.Type
(dlv) print request.ResourceID
(dlv) print request.NodeID

# 查看局部变量
(dlv) locals

# 查看调用栈
(dlv) stack
# 或简写
(dlv) bt

# 查看 goroutine 信息
(dlv) goroutines

# 单步执行
(dlv) next        # 下一行（不进入函数）
(dlv) step        # 下一行（进入函数）
(dlv) continue    # 继续执行到下一个断点（简写：c）
```

---

## 客户端调试

### 方法1：直接调试测试程序

```bash
# 调试 test-client-multi-layer.go
dlv debug test-client-multi-layer.go
```

### 方法2：调试 contentv2

```bash
cd contentv2
dlv debug
```

### 设置断点并运行

```bash
# 启动 Delve
dlv debug test-client-multi-layer.go

# 在 Delve 中设置断点
(dlv) break main.main
(dlv) break client/client.go:74      # tryLockOnce
(dlv) break client/client.go:146     # waitForLock
(dlv) break client/client.go:260     # handleOperationEvent
(dlv) break client/client.go:434     # tryUnlockOnce
(dlv) break test-client-multi-layer.go:39   # Lock 调用后
(dlv) break test-client-multi-layer.go:55   # 获得锁后

# 查看所有断点
(dlv) breakpoints

# 运行程序（需要先启动服务端）
(dlv) continue
```

**注意**：调试客户端前，需要先启动服务端（在另一个终端）：

```bash
# 终端1：启动服务端（非调试模式）
cd server
go run main.go handler.go lock_manager.go types.go sse_subscriber.go
```

### 调试客户端代码

当程序停在断点时：

```bash
# 查看当前代码位置
(dlv) list

# 查看变量
(dlv) print request
(dlv) print request.Type
(dlv) print request.ResourceID
(dlv) print result
(dlv) print result.Acquired
(dlv) print result.Error

# 查看调用栈
(dlv) stack

# 查看 goroutine
(dlv) goroutines

# 单步执行
(dlv) next
(dlv) step
(dlv) continue
```

---

## 同时调试客户端和服务端

### 方法：使用两个 Delve 实例

**终端1：调试服务端**

```bash
cd server
dlv debug

# 设置断点
(dlv) break handler.go:41
(dlv) break lock_manager.go:67
(dlv) break lock_manager.go:128
(dlv) break lock_manager.go:329

# 运行
(dlv) continue
```

**终端2：调试客户端**

```bash
# 启动 Delve
dlv debug test-client-multi-layer.go

# 设置断点
(dlv) break client/client.go:74
(dlv) break client/client.go:146
(dlv) break client/client.go:260
(dlv) break client/client.go:434

# 运行
(dlv) continue
```

**调试流程**：

1. 两个 Delve 都运行后，客户端会发送请求到服务端
2. 服务端 Delve 会停在 `handler.go:41`（Lock 处理）
3. 在服务端 Delve 中查看请求：`print request`，然后 `continue`
4. 客户端 Delve 会停在 `client/client.go:74`（tryLockOnce）
5. 在客户端 Delve 中查看响应：`print lockResp`，然后 `continue`
6. 继续这个过程，观察客户端和服务端的交互

---

## 常用调试场景

### 场景1：调试锁获取流程

**服务端断点**：
```bash
(dlv) break lock_manager.go:67      # TryLock
(dlv) break lock_manager.go:76      # 检查锁是否存在
(dlv) break lock_manager.go:116     # 创建新锁
```

**客户端断点**：
```bash
(dlv) break client/client.go:74     # tryLockOnce
(dlv) break client/client.go:134     # 处理响应
```

**调试步骤**：
1. 客户端发送请求 → 服务端停在 `TryLock`
2. 查看服务端锁状态：`print shard.locks[key]` → `continue`
3. 客户端收到响应 → 客户端停在 `tryLockOnce`
4. 查看客户端结果：`print lockResp` → `continue`

### 场景2：调试 SSE 订阅和事件广播

**服务端断点**：
```bash
(dlv) break handler.go:148          # Subscribe
(dlv) break lock_manager.go:275     # Subscribe（LockManager）
(dlv) break lock_manager.go:128     # Unlock
(dlv) break lock_manager.go:329     # broadcastEvent
```

**客户端断点**：
```bash
(dlv) break client/client.go:146    # waitForLock
(dlv) break client/client.go:260     # handleOperationEvent
```

**调试步骤**：
1. 客户端订阅 → 服务端停在 `Subscribe`
2. 查看订阅者注册：`print subscriber` → `continue`
3. 客户端等待事件 → 客户端停在 `waitForLock`
4. 服务端解锁 → 服务端停在 `Unlock`
5. 查看解锁逻辑：`print request` → `continue`
6. 服务端广播事件 → 服务端停在 `broadcastEvent`
7. 查看事件：`print event` → `continue`
8. 客户端收到事件 → 客户端停在 `handleOperationEvent`
9. 查看事件内容：`print event` → `continue`

### 场景3：调试错误处理

**设置条件断点**：
```bash
# 只在有错误时触发
(dlv) break lock_manager.go:67 if errMsg != ""
(dlv) break client/client.go:125 if lockResp.Error != ""
```

### 场景4：调试特定资源或节点

**设置条件断点**：
```bash
# 只在特定资源时触发
(dlv) break lock_manager.go:67 if request.ResourceID == "sha256:test123"
(dlv) break handler.go:41 if request.NodeID == "node-1"
```

---

## Delve 常用命令速查

```bash
# 基本命令
(dlv) continue         # 继续执行（简写：c）
(dlv) next             # 下一行，不进入函数（简写：n）
(dlv) step             # 下一行，进入函数（简写：s）
(dlv) stepout          # 执行到函数返回（简写：so）
(dlv) restart          # 重新启动程序（简写：r）

# 查看信息
(dlv) list             # 显示代码（简写：l）
(dlv) stack             # 查看调用栈（简写：bt）
(dlv) args              # 查看函数参数
(dlv) locals            # 查看局部变量
(dlv) vars              # 查看所有变量
(dlv) goroutines        # 查看所有 goroutine（简写：gr）
(dlv) thread            # 切换到指定线程

# 查看变量
(dlv) print variable    # 打印变量（简写：p）
(dlv) print *pointer    # 打印指针指向的内容
(dlv) whatis variable   # 查看变量类型

# 断点管理
(dlv) break file.go:line           # 设置断点（简写：b）
(dlv) break function_name          # 在函数设置断点
(dlv) break file.go:line if cond  # 条件断点
(dlv) clear N                      # 删除断点
(dlv) clearall                     # 删除所有断点
(dlv) breakpoints                  # 查看所有断点（简写：bp）

# Goroutine 调试（Go 特有）
(dlv) goroutines                   # 查看所有 goroutine
(dlv) goroutine N                  # 切换到 goroutine N
(dlv) goroutine N stack            # 查看 goroutine N 的调用栈
(dlv) goroutine N locals           # 查看 goroutine N 的局部变量

# 其他有用命令
(dlv) source script.dlv           # 执行脚本文件
(dlv) disassemble                  # 反汇编当前函数
(dlv) exit                         # 退出调试器（简写：q）
```

---

## 完整调试示例

### 示例：调试完整的锁获取和释放流程

**终端1：服务端 Delve**
```bash
cd server
dlv debug

(dlv) break handler.go:41
(dlv) break lock_manager.go:67
(dlv) break lock_manager.go:128
(dlv) break lock_manager.go:329
(dlv) continue
```

**终端2：客户端 Delve**
```bash
dlv debug test-client-multi-layer.go

(dlv) break client/client.go:74
(dlv) break client/client.go:146
(dlv) break client/client.go:260
(dlv) break client/client.go:434
(dlv) continue
```

**调试流程**：
1. 客户端发送加锁请求 → 服务端停在 `handler.go:41`
   - 查看请求：`print request`
   - 继续：`continue`
2. 服务端处理锁 → 停在 `lock_manager.go:67`
   - 查看锁状态：`print shard.locks[key]`
   - 继续：`continue`
3. 客户端收到响应 → 客户端停在 `client/client.go:74`
   - 查看响应：`print lockResp`
   - 继续：`continue`
4. 客户端发送解锁请求 → 服务端停在 `lock_manager.go:128`
   - 查看解锁逻辑：`print request`
   - 继续：`continue`
5. 服务端广播事件 → 停在 `lock_manager.go:329`
   - 查看事件：`print event`
   - 查看订阅者：`print subscribers`
   - 继续：`continue`
6. 客户端收到事件 → 客户端停在 `client/client.go:260`
   - 查看事件：`print event`
   - 继续：`continue`

---

## Delve 配置文件

创建 `.dlv/config.json` 文件：

```json
{
    "maxStringLen": 64,
    "maxArrayValues": 64,
    "maxVariableRecurse": 1,
    "maxStructFields": -1,
    "showGlobalVariables": false,
    "substitutePath": [
        {
            "from": "/build/path",
            "to": "/source/path"
        }
    ]
}
```

---

## Delve 脚本示例

创建 `debug.dlv` 脚本文件：

```bash
# 设置断点
break handler.go:41
break lock_manager.go:67
break lock_manager.go:128

# 运行程序
continue

# 定义命令别名
alias show_request = print request
alias show_lock = print shard.locks[key]
alias show_event = print event
```

使用脚本：
```bash
dlv debug --init debug.dlv
```

---

## VS Code 集成

Delve 与 VS Code 完美集成：

1. 安装 Go 扩展
2. 创建 `.vscode/launch.json`：

```json
{
    "version": "0.2.0",
    "configurations": [
        {
            "name": "Debug Server",
            "type": "go",
            "request": "launch",
            "mode": "debug",
            "program": "${workspaceFolder}/server",
            "env": {
                "PORT": "8086"
            }
        },
        {
            "name": "Debug Client",
            "type": "go",
            "request": "launch",
            "mode": "debug",
            "program": "${workspaceFolder}/test-client-multi-layer.go"
        }
    ]
}
```

3. 按 F5 开始调试，可以在代码中直接设置断点

---

## 常见问题

### 问题1：找不到 dlv 命令

**解决**：
```bash
# 确保 $GOPATH/bin 在 PATH 中
export PATH=$PATH:$(go env GOPATH)/bin

# 或使用完整路径
~/go/bin/dlv debug
```

### 问题2：无法设置断点

**原因**：代码路径不正确或文件不存在

**解决**：
```bash
# 检查文件路径
(dlv) list

# 使用完整路径设置断点
(dlv) break /full/path/to/file.go:line
```

### 问题3：变量显示为 nil

**原因**：变量在当前作用域不可见

**解决**：
```bash
# 查看当前作用域
(dlv) locals
(dlv) args

# 查看调用栈，切换到正确的帧
(dlv) stack
(dlv) frame N
```

### 问题4：Goroutine 调试

**查看所有 goroutine**：
```bash
(dlv) goroutines
```

**切换到特定 goroutine**：
```bash
(dlv) goroutine 2
(dlv) stack
(dlv) locals
```

---

## Delve vs GDB

| 特性 | Delve | GDB |
|------|-------|-----|
| Go 支持 | ✅ 原生支持 | ⚠️ 需要适配 |
| Goroutine | ✅ 完美支持 | ❌ 支持有限 |
| Interface | ✅ 完美支持 | ❌ 支持有限 |
| Channel | ✅ 完美支持 | ❌ 支持有限 |
| 变量查看 | ✅ 直观 | ⚠️ 需要技巧 |
| 安装 | ✅ 简单 | ⚠️ 需要配置 |

**推荐**：对于 Go 程序，使用 Delve 而不是 GDB。

---

## 总结

1. **安装 Delve**：`go install github.com/go-delve/delve/cmd/dlv@latest`
2. **启动调试**：`dlv debug`（自动编译）或 `dlv exec ./binary`
3. **设置断点**：`break file.go:line` 或 `break function_name`
4. **运行程序**：`continue`
5. **查看变量**：`print`、`locals`、`args`
6. **单步执行**：`next`、`step`、`continue`
7. **Goroutine 调试**：`goroutines`、`goroutine N`

Delve 是 Go 程序调试的最佳选择！

