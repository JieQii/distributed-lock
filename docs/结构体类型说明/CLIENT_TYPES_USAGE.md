# 客户端类型使用情况分析

## 1. 结构体使用情况

### ✅ 在客户端和 contentv2 中都使用的类型

#### `Request` - 锁请求结构
- **客户端使用**：`client.go` 中所有锁操作都使用此结构
- **contentv2 使用**：`store.go` 中创建锁请求，`writer.go` 中保存请求用于解锁
- **用途**：封装锁请求的所有信息（类型、资源ID、节点ID、错误、成功标志）

#### `LockResult` - 加锁结果
- **客户端使用**：`Lock()` 方法的返回值
- **contentv2 使用**：`store.go` 中接收 `ClusterLock()` 的返回值，判断是否获得锁或跳过
- **用途**：返回锁获取的结果（是否获得、是否跳过、错误信息）

#### `OperationTypePull/Update/Delete` - 操作类型常量
- **客户端使用**：`client.go` 中用于指定操作类型
- **contentv2 使用**：`store.go` 中使用 `OperationTypePull` 创建请求
- **用途**：定义操作类型（拉取、更新、删除）

### 🔒 仅在客户端内部使用的类型

#### `LockResponse` - 加锁响应
- **使用位置**：`client.go` 的 `tryLockOnce()` 和 `handleOperationEvent()` 中
- **用途**：解析服务端 `/lock` 接口返回的 JSON 响应
- **说明**：这是客户端与服务端通信的中间结构，不暴露给 contentv2

#### `UnlockResponse` - 解锁响应
- **使用位置**：`client.go` 的 `tryUnlockOnce()` 中
- **用途**：解析服务端 `/unlock` 接口返回的 JSON 响应
- **说明**：这是客户端与服务端通信的中间结构，不暴露给 contentv2

#### `OperationEvent` - 操作完成事件
- **使用位置**：`client.go` 的 `waitForLock()` 和 `handleOperationEvent()` 中
- **用途**：解析 SSE 订阅流中的事件数据
- **说明**：仅在客户端内部用于处理订阅者模式的事件推送

## 2. 未使用的类型

目前 `types.go` 中的所有类型都有使用，没有完全未使用的类型。

## 3. 类型依赖关系

```
contentv2 (调用方)
    ↓
ClusterLock/ClusterUnLock (公共接口)
    ↓
LockClient.Lock/Unlock (客户端方法)
    ↓
tryLockOnce/waitForLock/tryUnlockOnce (内部实现)
    ↓
LockResponse/UnlockResponse/OperationEvent (与服务端通信)
```

## 4. 总结

- **对外暴露的类型**：`Request`、`LockResult`、操作类型常量
- **内部使用的类型**：`LockResponse`、`UnlockResponse`、`OperationEvent`
- **设计原则**：contentv2 只需要知道如何创建请求和处理结果，不需要了解与服务端通信的细节

