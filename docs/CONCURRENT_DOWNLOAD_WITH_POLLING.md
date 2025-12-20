# 并发下载 + 轮询跳过机制

## 需求

当节点A和节点B同时请求下载镜像时：
1. 节点A开始下载镜像层1
2. 节点B请求层1时，应该：
   - 被告知层1已经有人在下载（加入等待队列）
   - 可以立即去下载层2（不同层可以并发）
   - 同时轮询层1是否下载成功，如果成功就跳过层1的下载

## 实现机制 ✅

### 1. 服务器返回明确信息

当节点B请求层1时，服务器返回：
```json
{
  "acquired": false,
  "skip": false,
  "message": "锁已被占用，已加入等待队列"
}
```

**含义**：
- `acquired=false`: 未获得锁
- `skip=false`: 操作未完成，不能跳过
- `message`: 明确告知已加入等待队列

### 2. 客户端并发处理不同层

**关键点**：每个层是独立的资源，使用不同的锁。

**实现**：
```go
// 并发下载所有层
for _, layerID := range layers {
    go func(layer string) {
        // 每个层独立获取锁
        writer, err := lockintegration.OpenWriter(ctx, serverURL, nodeID, layer)
        // ...
    }(layerID)
}
```

**结果**：
- 层1被节点A占用 → 节点B加入等待队列
- 层2未被占用 → 节点B立即获得锁，开始下载
- 层3未被占用 → 节点B立即获得锁，开始下载

### 3. 轮询机制发现操作已完成

**实现**：`client/client.go` 中的 `waitForLock()` 方法

```go
func (c *LockClient) waitForLock(ctx context.Context, request *Request) (*LockResult, error) {
    ticker := time.NewTicker(500 * time.Millisecond) // 每500ms轮询一次
    defer ticker.Stop()

    for {
        select {
        case <-ctx.Done():
            return nil, ctx.Err()
        case <-ticker.C:
            // 查询锁状态
            statusResp := queryLockStatus(...)
            
            // 如果操作已完成且成功，跳过操作
            if statusResp.Completed && statusResp.Success {
                return &LockResult{
                    Acquired: false,
                    Skipped:  true,
                }, nil
            }
        }
    }
}
```

**流程**：
1. 节点B请求层1，未获得锁
2. 进入 `waitForLock()` 轮询
3. 每500ms查询一次 `/lock/status`
4. 如果发现 `completed=true && success=true`，返回 `skipped=true`
5. 客户端跳过层1的下载

## 完整流程示例

### 时间线

```
T0: 节点A请求层1 → 获得锁 ✅
   节点A开始下载层1

T1: 节点B请求层1 → 加入等待队列 ⏳
   节点B请求层2 → 获得锁 ✅
   节点B开始下载层2

T2: 节点B完成层2 → 释放锁 ✅
   节点B继续轮询层1的状态

T3: 节点A完成层1 → 释放锁（成功）✅
   锁标记为 completed=true, success=true

T4: 节点B轮询发现层1已完成 → 跳过下载 ✅
   节点B继续下载其他层（层3、层4等）
```

### 代码示例

```go
// 节点B的下载逻辑
func DownloadImageLayers(ctx context.Context, serverURL, nodeID string, layers []string) error {
    var wg sync.WaitGroup
    
    // 并发下载所有层
    for _, layerID := range layers {
        wg.Add(1)
        go func(layer string) {
            defer wg.Done()
            
            // 打开Writer（会尝试获取锁）
            writer, err := lockintegration.OpenWriter(ctx, serverURL, nodeID, layer)
            if err != nil {
                log.Printf("层 %s 获取锁失败: %v", layer, err)
                return
            }
            defer writer.Close(ctx)
            
            // 检查是否跳过了操作（其他节点已完成）
            if writer.Skipped() {
                log.Printf("层 %s 已由其他节点完成，跳过下载", layer)
                return // 跳过下载，直接返回成功
            }
            
            // 检查是否获得锁
            if !writer.Locked() {
                log.Printf("层 %s 未获得锁", layer)
                return
            }
            
            // 下载层
            log.Printf("开始下载层 %s", layer)
            if err := downloadLayer(layer); err != nil {
                writer.Commit(ctx, false, err)
                return
            }
            
            // 提交成功结果
            writer.Commit(ctx, true, nil)
            log.Printf("层 %s 下载完成", layer)
        }(layerID)
    }
    
    wg.Wait()
    return nil
}
```

## 测试用例

### Go单元测试

**文件**: `server/lock_manager_test.go`

**测试函数**: `TestNodeConcurrentDifferentResources`

**验证点**：
1. ✅ 节点B在等待队列中时，能够并发获得其他层的锁
2. ✅ 节点B可以同时处理多个层的下载
3. ✅ 节点B通过轮询发现层1已完成，跳过下载

### Shell脚本测试

**文件**: `test-concurrent-download-with-polling.sh`

**测试步骤**：
1. 节点A请求层1并获得锁
2. 节点B请求层1，加入等待队列
3. 节点B请求层2，立即获得锁
4. 节点B完成层2，释放锁
5. 节点B轮询层1的状态
6. 节点A完成层1，释放锁（成功）
7. 节点B通过轮询发现层1已完成，跳过下载

## 关键特性

### ✅ 1. 并发下载不同层

- 每个层是独立的资源
- 不同的层可以并发下载
- 不会因为一个层的等待而阻塞其他层

### ✅ 2. 轮询发现操作已完成

- 等待的节点通过轮询 `/lock/status` 发现操作已完成
- 如果操作成功，跳过下载
- 避免重复下载

### ✅ 3. 非阻塞设计

- 节点在等待一个层时，可以继续下载其他层
- 提高下载效率
- 充分利用多节点资源

## 使用示例

### 示例代码

**文件**: `examples/concurrent_layer_download.go`

```go
// 节点A和节点B同时下载镜像
func ExampleConcurrentDownload() {
    ctx := context.Background()
    serverURL := "http://localhost:8080"
    
    layers := []string{
        "sha256:layer1",
        "sha256:layer2",
        "sha256:layer3",
        "sha256:layer4",
    }
    
    // 节点A开始下载
    go DownloadImageLayers(ctx, serverURL, "NODEA", layers)
    
    // 节点B开始下载（稍后开始）
    time.Sleep(100 * time.Millisecond)
    DownloadImageLayers(ctx, serverURL, "NODEB", layers)
}
```

### 运行测试

```bash
# 1. 启动服务器
cd server
go run main.go

# 2. 运行测试脚本
cd ..
./test-concurrent-download-with-polling.sh
```

## 总结

### ✅ 已实现的功能

1. **服务器返回明确信息**
   - 告知节点B层1已加入等待队列
   - 节点B可以继续请求其他层

2. **客户端并发处理**
   - 节点B可以同时处理多个层的下载
   - 不同层的锁是独立的

3. **轮询机制**
   - 节点B通过轮询发现层1已完成
   - 自动跳过已完成的层

### 优势

- ✅ **提高效率**：节点不会因为一个层的等待而阻塞
- ✅ **避免重复**：通过轮询发现操作已完成，跳过下载
- ✅ **充分利用资源**：不同层可以并发下载

### 实现位置

- **服务器端**: `server/handler.go` - Lock接口返回明确信息
- **客户端**: `client/client.go` - waitForLock轮询机制
- **集成层**: `conchContent-v3/lockintegration/writer.go` - OpenWriter处理跳过逻辑

