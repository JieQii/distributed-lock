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
(dlv) break lock_manager.go:129    # Unlock 函数（注意：128 行是注释，使用 129 行）
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

**重要：文件位置要求**

`test-client-multi-layer.go` 文件必须放在**项目根目录**（与 `go.mod` 同级），因为：
- 文件使用了 `import "distributed-lock/client"` 导入路径
- 模块名是 `distributed-lock`（在 `go.mod` 中定义）
- Go 模块系统要求从项目根目录解析导入路径

**当前文件位置**：`c:\Users\admin\Desktop\distributed-lock\test-client-multi-layer.go` ✅（正确）

**调试步骤**：

```bash
# 1. 确保在项目根目录
cd c:\Users\admin\Desktop\distributed-lock

# 2. 调试 test-client-multi-layer.go
dlv debug test-client-multi-layer.go
```

**如果文件不在根目录**：
- ❌ 如果放在子目录（如 `examples/`），导入路径会找不到 `distributed-lock/client`
- ✅ 解决方案：将文件移动到项目根目录，或修改导入路径

### 特殊情况：只有 client 文件夹和测试文件

如果你的目录结构是：
```
your-folder/
  ├── test-client-multi-layer.go
  └── client/
      ├── client.go
      └── types.go
```

**需要创建 `go.mod` 文件**：

1. **在 `your-folder/` 目录下创建 `go.mod`**：
```bash
cd your-folder
go mod init distributed-lock
```

2. **如果 client 包依赖其他包，运行**：
```bash
go mod tidy
```

3. **然后就可以正常调试**：
```bash
dlv debug test-client-multi-layer.go
```

**目录结构应该是**：
```
your-folder/
  ├── go.mod              # ← 必须创建
  ├── test-client-multi-layer.go
  └── client/
      ├── client.go
      └── types.go
```

**验证方法**：
```bash
# 在 your-folder 目录下运行
go build test-client-multi-layer.go
# 如果编译成功，说明配置正确
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
(dlv) break lock_manager.go:129    # Unlock（注意：128 行是注释）
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

### 场景2：调试 SSE 订阅和事件广播（详细流程）

#### 2.1 订阅请求后的完整流程

**当客户端发送订阅请求后会发生什么：**

1. **客户端发送订阅请求** (`client/client.go:160`)
   - 构建订阅 URL：`/lock/subscribe?type=pull&resource_id=sha256:xxx`
   - 设置 SSE 请求头：`Accept: text/event-stream`
   - 发送 GET 请求并保持连接打开

2. **服务端接收订阅请求** (`server/handler.go:144`)
   - 解析查询参数：`type` 和 `resource_id`
   - 设置 SSE 响应头：`Content-Type: text/event-stream`
   - 创建 `SSESubscriber` 实例
   - 注册订阅者到 `LockManager`
   - **保持连接打开，等待事件推送**

3. **订阅者注册** (`server/lock_manager.go:277`)
   - 将订阅者添加到对应资源的订阅者列表
   - 订阅者列表存储在 `shard.subscribers[key]` 中
   - 连接保持打开状态，等待后续事件

4. **客户端等待事件** (`client/client.go:184`)
   - 使用 `bufio.Scanner` 读取 SSE 流
   - 解析 SSE 格式：`data: {json}\n\n`
   - 等待服务端推送事件

#### 2.2 验证操作成功后的广播流程

**完整验证步骤：**

**步骤1：设置断点**

**服务端断点**：
```bash
(dlv) break handler.go:148          # Subscribe 处理函数
(dlv) break lock_manager.go:277     # Subscribe（注册订阅者）
(dlv) break lock_manager.go:129    # Unlock（注意：128 行是注释）     # Unlock（操作完成）
(dlv) break lock_manager.go:161     # 操作成功后的广播触发点
(dlv) break lock_manager.go:325     # broadcastEvent（广播函数）
(dlv) break sse_subscriber.go:29    # SendEvent（发送事件给客户端）
```

**客户端断点**：
```bash
(dlv) break client/client.go:160    # 创建订阅请求
(dlv) break client/client.go:168    # 发送订阅请求
(dlv) break client/client.go:184    # 开始读取 SSE 流
(dlv) break client/client.go:200     # 收到事件并解析
(dlv) break client/client.go:252     # handleOperationEvent（处理事件）
```

**步骤2：启动调试**

**终端1：启动服务端调试**
```bash
cd server
dlv debug

# 设置断点
(dlv) break handler.go:148
(dlv) break lock_manager.go:277
(dlv) break lock_manager.go:129    # Unlock（注意：128 行是注释）
(dlv) break lock_manager.go:161
(dlv) break lock_manager.go:325
(dlv) break sse_subscriber.go:29

# 运行
(dlv) continue
```

**终端2：启动客户端调试**
```bash
dlv debug test-client-multi-layer.go

# 设置断点
(dlv) break client/client.go:160
(dlv) break client/client.go:168
(dlv) break client/client.go:184
(dlv) break client/client.go:200
(dlv) break client/client.go:252

# 运行
(dlv) continue
```

**步骤3：验证订阅请求流程**

1. **客户端发送订阅请求**
   - 客户端停在 `client/client.go:160`
   - 查看订阅 URL：`print subscribeURL`
   - 继续：`continue`

2. **服务端接收订阅请求**
   - 服务端停在 `handler.go:148`
   - 查看请求参数：
     ```bash
     (dlv) print typeParam
     (dlv) print resourceIDParam
     ```
   - 继续：`continue`

3. **注册订阅者**
   - 服务端停在 `lock_manager.go:277`
   - 查看订阅者列表：
     ```bash
     (dlv) print key
     (dlv) print len(shard.subscribers[key])
     ```
   - 继续：`continue`

4. **客户端建立连接**
   - 客户端停在 `client/client.go:168`
   - 查看响应状态：`print resp.StatusCode`
   - 继续：`continue`

5. **客户端开始等待事件**
   - 客户端停在 `client/client.go:184`
   - 此时连接已建立，等待服务端推送事件
   - 继续：`continue`（程序会在这里等待）

**步骤4：触发操作并验证广播**

**终端3：模拟另一个节点完成操作**
```bash
# 节点1获取锁并完成操作
curl -X POST http://localhost:8086/lock \
  -H "Content-Type: application/json" \
  -d '{"type":"pull","resource_id":"sha256:test123","node_id":"node-1"}'

# 等待一段时间（模拟操作执行）

# 节点1释放锁（操作成功）
curl -X POST http://localhost:8086/unlock \
  -H "Content-Type: application/json" \
  -d '{"type":"pull","resource_id":"sha256:test123","node_id":"node-1","error":""}'
```

**验证流程：**

1. **服务端处理解锁请求**
   - 服务端停在 `lock_manager.go:128`
   - 查看解锁请求：
     ```bash
     (dlv) print request
     (dlv) print request.NodeID
     (dlv) print request.Error
     ```
   - 继续：`continue`

2. **服务端判断操作成功**
   - 服务端停在 `lock_manager.go:161`（操作成功分支）
   - 查看锁状态：
     ```bash
     (dlv) print lockInfo.Success
     (dlv) print lockInfo.Completed
     ```
   - 继续：`continue`

3. **服务端触发广播**
   - 服务端停在 `lock_manager.go:325`（broadcastEvent）
   - 查看事件和订阅者：
     ```bash
     (dlv) print event
     (dlv) print event.Success
     (dlv) print event.NodeID
     (dlv) print len(subscribers)
     ```
   - 继续：`continue`

4. **服务端发送事件给订阅者**
   - 服务端停在 `sse_subscriber.go:29`（SendEvent）
   - 查看发送的事件：
     ```bash
     (dlv) print event
     (dlv) print eventJSON
     ```
   - 继续：`continue`（会为每个订阅者触发一次）

5. **客户端收到事件**
   - 客户端停在 `client/client.go:200`（解析事件）
   - 查看解析的事件：
     ```bash
     (dlv) print currentEventJSON
     (dlv) print event
     ```
   - 继续：`continue`

6. **客户端处理事件**
   - 客户端停在 `client/client.go:252`（handleOperationEvent）
   - 查看事件详情：
     ```bash
     (dlv) print event
     (dlv) print event.Success
     (dlv) print event.NodeID
     (dlv) print event.ResourceID
     ```
   - 查看处理结果：
     ```bash
     (dlv) print result
     (dlv) print done
     ```
   - 继续：`continue`

#### 2.3 关键验证点

**验证订阅者已注册：**
```bash
# 在服务端 Delve 中
(dlv) break lock_manager.go:289
(dlv) continue
# 触发订阅后
(dlv) print len(shard.subscribers[key])
# 应该显示订阅者数量 > 0
```

**验证事件内容：**
```bash
# 在服务端 broadcastEvent 断点处
(dlv) print event.Type
(dlv) print event.ResourceID
(dlv) print event.NodeID
(dlv) print event.Success
(dlv) print event.CompletedAt
```

**验证客户端收到事件：**
```bash
# 在客户端 handleOperationEvent 断点处
(dlv) print event.Success
# 如果 success=true，应该返回错误提示检查资源
(dlv) print result.Error
```

#### 2.4 常见问题排查

**问题1：客户端没有收到事件**

**检查点**：
1. 确认订阅者已注册：
   ```bash
   # 在 lock_manager.go:289 断点处
   (dlv) print len(shard.subscribers[key])
   ```
2. 确认广播被触发：
   ```bash
   # 在 lock_manager.go:325 断点处
   (dlv) print len(subscribers)
   ```
3. 确认事件发送成功：
   ```bash
   # 在 sse_subscriber.go:29 断点处
   (dlv) print err
   # 应该为 nil
   ```

**问题2：事件内容不正确**

**检查点**：
1. 在 `lock_manager.go:161` 查看创建的事件：
   ```bash
   (dlv) print event
   ```
2. 在 `sse_subscriber.go:38` 查看序列化后的 JSON：
   ```bash
   (dlv) print string(eventJSON)
   ```

**问题3：多个订阅者只收到部分事件**

**检查点**：
1. 在 `lock_manager.go:337` 查看循环：
   ```bash
   (dlv) print len(subscribers)
   (dlv) print i
   ```
2. 检查每个订阅者的发送结果：
   ```bash
   (dlv) print err
   ```

#### 2.5 快速验证广播功能（简化版）

如果你只想快速验证广播是否工作，可以使用以下简化步骤：

**步骤1：设置关键断点**

**服务端**：
```bash
(dlv) break lock_manager.go:325     # broadcastEvent
(dlv) break sse_subscriber.go:29     # SendEvent
```

**客户端**：
```bash
(dlv) break client/client.go:252     # handleOperationEvent
```

**步骤2：运行并观察**

1. 启动两个节点（一个获取锁，一个订阅等待）
2. 当获取锁的节点完成操作并调用 Unlock 时：
   - 服务端会停在 `broadcastEvent`
   - 查看订阅者数量：`print len(subscribers)`
   - 继续：`continue`
3. 服务端会停在 `SendEvent`（每个订阅者一次）
   - 查看事件：`print event`
   - 继续：`continue`
4. 客户端会停在 `handleOperationEvent`
   - 查看收到的事件：`print event`
   - 验证 `event.Success` 是否为 `true`
   - 继续：`continue`

**验证成功标志**：
- ✅ 服务端 `broadcastEvent` 被调用
- ✅ 订阅者数量 > 0
- ✅ `SendEvent` 被调用（次数 = 订阅者数量）
- ✅ 客户端 `handleOperationEvent` 被调用
- ✅ 客户端收到的 `event.Success == true`

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
(dlv) break lock_manager.go:129    # Unlock（注意：128 行是注释）
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
break lock_manager.go:129    # Unlock（注意：128 行是注释）

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

### 问题5：调试时客户端请求超时

**问题现象**：
```
请求超时: Post "http://127.0.0.1:8086/lock": context deadline exceeded
```

**原因分析**：
1. 服务端在调试模式下，当停在断点时，**无法处理新的 HTTP 请求**
2. 客户端设置了超时时间（如 5 秒），当服务端长时间停在断点时，请求会超时
3. 客户端会重试，但服务端仍然停在断点，导致所有请求都超时

**解决方案**：

**方案1：快速通过断点（推荐）**
```bash
# 在服务端调试时，不要长时间停在断点
# 快速查看变量后立即 continue
(dlv) print request
(dlv) continue  # 立即继续，不要长时间停留
```

**方案2：增加客户端超时时间**
```go
// 在 test-client-multi-layer.go 中修改
clientA.RequestTimeout = 60 * time.Second  // 从 5 秒增加到 60 秒
clientB.RequestTimeout = 60 * time.Second
```

**方案3：使用条件断点**
```bash
# 只在特定条件下停止（减少不必要的停止）
(dlv) break lock_manager.go:67 if request.NodeID == "NODEA"
(dlv) break lock_manager.go:129 if request.NodeID == "NODEA"
```

**方案4：先让服务端正常运行，只在关键点设置断点**
```bash
# 只设置最重要的断点
(dlv) break lock_manager.go:325  # 只在广播事件时停止
(dlv) break sse_subscriber.go:29  # 只在发送事件时停止
```

**方案5：使用日志而不是断点**
```bash
# 在代码中添加日志，而不是设置断点
log.Printf("[DEBUG] 收到请求: %+v", request)
```

**调试最佳实践**：
1. ✅ **快速查看变量**：`print variable` → 立即 `continue`
2. ✅ **使用条件断点**：只在需要时停止
3. ✅ **增加超时时间**：调试时设置更长的超时
4. ✅ **分步调试**：先调试服务端，再调试客户端
5. ❌ **避免长时间停在断点**：会导致客户端请求超时

**验证方法**：
```bash
# 在服务端调试时，快速通过断点
(dlv) break handler.go:41
(dlv) continue
# 当停在断点时，快速执行：
(dlv) print request
(dlv) continue  # 立即继续，不要停留超过 1 秒
```

**注意**：断点行号问题
```bash
# 如果断点设置失败，可能是行号不对
(dlv) break lock_manager.go:129    # Unlock（注意：128 行是注释）
# Command failed: could not find statement at lock_manager.go:128

# 解决：查看实际代码，找到可执行语句的行号
(dlv) list lock_manager.go:125
# 128 行可能是注释或空行，使用 129 行
(dlv) break lock_manager.go:129
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

