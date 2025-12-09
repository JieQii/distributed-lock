<<<<<<< HEAD
# 分布式锁系统

用于管理镜像层下载的分布式锁系统，确保同一镜像层在同一时刻只有一个节点能够下载。

## 功能特性

1. **FIFO队列管理**：按照先进先出顺序管理锁请求，确保请求按顺序获得锁
2. **分布式锁**：确保在某一时刻只有一个节点能操作该资源（镜像层digest）
3. **自动释放**：操作成功或失败后自动释放锁，队列中的下一个请求可以获得锁
4. **HTTP协议**：通过HTTP协议实现客户端和服务端的通信

## 项目结构

```
.
├── server/          # 锁服务端
│   ├── main.go      # 服务端主程序
│   ├── lock_manager.go  # 锁管理器（FIFO队列、锁分配）
│   ├── handler.go   # HTTP请求处理器
│   └── types.go     # 服务端类型定义
├── client/          # 锁客户端
│   ├── client.go    # HTTP客户端实现
│   └── types.go     # 客户端类型定义
├── content/         # Content插件集成
│   ├── writer.go    # Writer实现（集成锁客户端）
│   └── example.go   # 使用示例
├── go.mod           # Go模块文件
└── README.md        # 项目说明文档
```

## 快速开始

### 1. 启动锁服务端

```bash
cd server
go run main.go
```

服务端默认监听在 `:8080` 端口，可以通过环境变量 `PORT` 修改端口号。

### 2. 在Content插件中使用

```go
import (
    "context"
    "distributed-lock/content"
)

ctx := context.Background()

// 打开Writer，自动获取锁
cw, err := content.OpenWriter(ctx, "http://localhost:8080", "node-1", "sha256:abc123...")
if err != nil {
    log.Fatal(err)
}
defer cw.Close(ctx)  // 确保释放锁

// 执行下载操作
if err := downloadLayer(); err != nil {
    cw.Commit(ctx, false, err)  // 操作失败
} else {
    cw.Commit(ctx, true, nil)   // 操作成功
}
```

## 工作流程

### 加锁流程

1. 节点A调用 `OpenWriter()` 尝试获取锁
2. 如果锁可用，节点A获得锁，开始下载操作
3. 如果锁被占用，节点A加入FIFO等待队列
4. 节点A轮询锁状态：
   - 如果操作已完成且成功，节点A跳过下载操作
   - 如果操作已完成但失败，节点A继续等待获取锁
   - 如果获得锁，节点A开始下载操作

### 解锁流程

1. 节点完成操作后调用 `Commit()` 或 `Close()`
2. 客户端发送解锁请求到服务端，携带操作结果（成功/失败）
3. 服务端释放锁：
   - 如果操作成功，保留锁信息一段时间（其他节点查询时会跳过操作）
   - 如果操作失败，立即释放锁并分配给队列中的下一个请求

## API接口

### 服务端接口

#### POST /lock
获取锁

请求体：
```json
{
  "type": "image-layer",
  "resource_id": "sha256:abc123...",
  "node_id": "node-1"
}
```

响应：
```json
{
  "acquired": true,
  "skip": false,
  "message": "成功获得锁"
}
```

#### POST /unlock
释放锁

请求体：
```json
{
  "type": "image-layer",
  "resource_id": "sha256:abc123...",
  "node_id": "node-1",
  "success": true,
  "error": ""
}
```

响应：
```json
{
  "released": true,
  "message": "成功释放锁"
}
```

#### GET /lock/status
查询锁状态

请求体：
```json
{
  "type": "image-layer",
  "resource_id": "sha256:abc123..."
}
```

响应：
```json
{
  "acquired": true,
  "completed": false,
  "success": false
}
```

## 使用场景示例

### 场景1：节点A和节点B同时请求下载层1

1. 节点A先发送请求，获得锁，开始下载层1
2. 节点B发送请求，锁被占用，加入等待队列
3. 节点A下载成功，调用 `Commit(ctx, true, nil)` 释放锁
4. 节点B获得锁，但查询发现操作已完成且成功，跳过下载操作

### 场景2：节点A下载层1，节点C下载层2

1. 节点A获得层1的锁，开始下载
2. 节点C获得层2的锁（不同资源，互不影响），开始下载
3. 两个节点可以同时下载不同的层

### 场景3：节点A下载失败

1. 节点A获得锁，开始下载
2. 节点B在等待队列中
3. 节点A下载失败，调用 `Commit(ctx, false, err)` 释放锁
4. 节点B获得锁，开始下载

## 注意事项

1. 确保每个节点有唯一的 `nodeID`
2. `resourceID` 应该是镜像层的digest，确保唯一性
3. 操作完成后必须调用 `Commit()` 或 `Close()` 释放锁
4. 服务端需要保证高可用性，建议使用负载均衡或集群部署

## 依赖

- Go 1.21+
- github.com/gorilla/mux

## 安装依赖

```bash
go mod download
```

=======
# distributed-lock
>>>>>>> 1a9907a493036a0c59d2b7be9a1feb37ae3b4d2a
