# 订阅者模式验证指南

## 概述

本文档介绍如何验证服务端的订阅者模式功能，包括单元测试和集成测试两种方式。

---

## 方式1：运行单元测试（推荐）

### 运行所有订阅者相关测试

```bash
cd server

# 运行所有订阅者测试
go test -v -run TestSubscriber

# 运行特定测试
go test -v -run TestSubscriberPattern
go test -v -run TestMultipleSubscribers
go test -v -run TestSubscriberHTTPEndpoint
go test -v -run TestSubscriberUnsubscribe
```

### 预期输出

**TestSubscriberPattern** 应该显示：
```
=== RUN   TestSubscriberPattern
    subscriber_test.go:XX: 订阅者成功收到事件: {Type:pull ResourceID:sha256:test123 NodeID:node-1 Success:true ...}
--- PASS: TestSubscriberPattern (X.XXs)
```

**TestMultipleSubscribers** 应该显示：
```
=== RUN   TestMultipleSubscribers
    subscriber_test.go:XX: 订阅者 0 收到事件: {Type:pull ResourceID:sha256:test456 NodeID:node-2 Success:true ...}
    subscriber_test.go:XX: 订阅者 1 收到事件: {Type:pull ResourceID:sha256:test456 NodeID:node-2 Success:true ...}
    subscriber_test.go:XX: 订阅者 2 收到事件: {Type:pull ResourceID:sha256:test456 NodeID:node-2 Success:true ...}
--- PASS: TestMultipleSubscribers (X.XXs)
```

---

## 方式2：手动集成测试

### 步骤1：启动服务端

```bash
cd server
go run main.go handler.go lock_manager.go types.go sse_subscriber.go

# 或编译后运行
go build -o lock-server
./lock-server
```

服务端应该启动在 `http://localhost:8086`

### 步骤2：在终端1中订阅事件（SSE）

```bash
# 订阅 pull 操作的完成事件
curl -N "http://localhost:8086/lock/subscribe?type=pull&resource_id=sha256:test123"

# 参数说明：
# -N: 不缓冲输出，实时显示
# type: 操作类型（pull/update/delete）
# resource_id: 资源ID
```

终端会保持连接，等待事件推送。

### 步骤3：在终端2中执行操作

```bash
# 1. 获取锁
curl -X POST http://localhost:8086/lock \
  -H "Content-Type: application/json" \
  -d '{
    "type": "pull",
    "resource_id": "sha256:test123",
    "node_id": "node-1"
  }'

# 2. 等待几秒（模拟操作时间）
sleep 2

# 3. 释放锁（操作成功）
curl -X POST http://localhost:8086/unlock \
  -H "Content-Type: application/json" \
  -d '{
    "type": "pull",
    "resource_id": "sha256:test123",
    "node_id": "node-1",
    "success": true
  }'
```

### 步骤4：验证订阅者收到事件

在终端1中，你应该看到类似以下输出：

```
data: {"type":"pull","resource_id":"sha256:test123","node_id":"node-1","success":true,"error":"","completed_at":"2024-01-01T12:00:00Z"}

```

---

## 方式3：使用 Python 脚本测试

创建一个测试脚本 `test_subscriber.py`：

```python
#!/usr/bin/env python3
import requests
import json
import threading
import time

SERVER_URL = "http://localhost:8086"
RESOURCE_ID = "sha256:test123"

def subscribe_events(resource_id):
    """订阅事件"""
    url = f"{SERVER_URL}/lock/subscribe"
    params = {
        "type": "pull",
        "resource_id": resource_id
    }
    
    print(f"开始订阅: {resource_id}")
    response = requests.get(url, params=params, stream=True)
    
    for line in response.iter_lines():
        if line:
            decoded_line = line.decode('utf-8')
            if decoded_line.startswith('data: '):
                event_json = decoded_line[6:]  # 移除 "data: " 前缀
                event = json.loads(event_json)
                print(f"收到事件: {json.dumps(event, indent=2)}")

def perform_operation(resource_id, node_id):
    """执行操作"""
    time.sleep(1)  # 等待订阅者连接
    
    # 获取锁
    print(f"\n节点 {node_id} 获取锁...")
    lock_response = requests.post(
        f"{SERVER_URL}/lock",
        json={
            "type": "pull",
            "resource_id": resource_id,
            "node_id": node_id
        }
    )
    print(f"获取锁结果: {lock_response.json()}")
    
    # 模拟操作
    time.sleep(2)
    
    # 释放锁（成功）
    print(f"\n节点 {node_id} 释放锁（成功）...")
    unlock_response = requests.post(
        f"{SERVER_URL}/unlock",
        json={
            "type": "pull",
            "resource_id": resource_id,
            "node_id": node_id,
            "success": True
        }
    )
    print(f"释放锁结果: {unlock_response.json()}")

if __name__ == "__main__":
    # 启动订阅者（在后台线程）
    subscriber_thread = threading.Thread(
        target=subscribe_events,
        args=(RESOURCE_ID,),
        daemon=True
    )
    subscriber_thread.start()
    
    # 等待订阅建立
    time.sleep(0.5)
    
    # 执行操作
    perform_operation(RESOURCE_ID, "node-1")
    
    # 等待事件接收
    time.sleep(1)
```

运行脚本：

```bash
python3 test_subscriber.py
```

---

## 方式4：使用 Go 客户端测试

创建一个测试程序 `test_subscriber_client.go`：

```go
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

func main() {
	serverURL := "http://localhost:8086"
	resourceID := "sha256:test123"

	// 启动订阅者（goroutine）
	go func() {
		subscribeURL := fmt.Sprintf("%s/lock/subscribe?type=pull&resource_id=%s",
			serverURL, resourceID)
		
		resp, err := http.Get(subscribeURL)
		if err != nil {
			fmt.Printf("订阅失败: %v\n", err)
			return
		}
		defer resp.Body.Close()

		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			line := scanner.Text()
			if len(line) > 6 && line[:6] == "data: " {
				eventJSON := line[6:]
				var event map[string]interface{}
				if err := json.Unmarshal([]byte(eventJSON), &event); err == nil {
					fmt.Printf("收到事件: %+v\n", event)
				}
			}
		}
	}()

	// 等待订阅建立
	time.Sleep(500 * time.Millisecond)

	// 执行操作
	lockReq := map[string]string{
		"type":       "pull",
		"resource_id": resourceID,
		"node_id":     "node-1",
	}
	lockJSON, _ := json.Marshal(lockReq)
	
	resp, _ := http.Post(serverURL+"/lock", "application/json",
		bytes.NewBuffer(lockJSON))
	resp.Body.Close()
	fmt.Println("获取锁成功")

	time.Sleep(2 * time.Second)

	unlockReq := map[string]interface{}{
		"type":       "pull",
		"resource_id": resourceID,
		"node_id":     "node-1",
		"success":    true,
	}
	unlockJSON, _ := json.Marshal(unlockReq)
	
	resp, _ = http.Post(serverURL+"/unlock", "application/json",
		bytes.NewBuffer(unlockJSON))
	resp.Body.Close()
	fmt.Println("释放锁成功")

	// 等待事件接收
	time.Sleep(1 * time.Second)
}
```

运行：

```bash
go run test_subscriber_client.go
```

---

## 验证要点

### 1. 订阅者注册成功

- 服务端日志应该显示：`[Subscribe] 添加订阅者: key=pull:sha256:test123, 当前订阅者数量=1`

### 2. 事件广播成功

- 服务端日志应该显示：`[BroadcastEvent] 广播事件: key=pull:sha256:test123, 订阅者数量=1, success=true`

### 3. 订阅者收到事件

- 客户端应该收到格式正确的 SSE 事件
- 事件应包含正确的 `type`、`resource_id`、`node_id`、`success` 字段

### 4. 多个订阅者

- 多个客户端订阅同一资源时，所有订阅者都应收到事件

### 5. 取消订阅

- 客户端断开连接时，订阅者应该被自动移除
- 服务端日志应该显示：`[Unsubscribe] 移除订阅者`

---

## 常见问题

### 1. 订阅者未收到事件

**检查**:
- 服务端是否正常运行
- 订阅 URL 是否正确
- 操作是否成功完成（`success=true`）
- 资源ID是否匹配

### 2. SSE 连接立即断开

**检查**:
- 服务端日志中的错误信息
- 网络连接是否稳定
- 客户端是否支持 SSE

### 3. 事件格式错误

**检查**:
- JSON 序列化是否正确
- SSE 格式是否正确（`data: {...}\n\n`）

---

## 性能测试

### 测试多个订阅者

```bash
# 同时启动多个订阅者
for i in {1..10}; do
  curl -N "http://localhost:8086/lock/subscribe?type=pull&resource_id=sha256:test" &
done

# 执行操作，观察所有订阅者是否都收到事件
```

### 测试并发操作

```bash
# 同时执行多个操作
for i in {1..5}; do
  curl -X POST http://localhost:8086/lock \
    -H "Content-Type: application/json" \
    -d "{\"type\":\"pull\",\"resource_id\":\"sha256:test$i\",\"node_id\":\"node-$i\"}" &
done
```

---

## 总结

订阅者模式的验证可以通过以下方式：

1. ✅ **单元测试** - 快速验证基本功能
2. ✅ **手动测试** - 使用 curl 验证端到端流程
3. ✅ **脚本测试** - 使用 Python/Go 脚本自动化测试
4. ✅ **性能测试** - 验证多订阅者和并发场景

选择最适合你当前需求的测试方式即可。

