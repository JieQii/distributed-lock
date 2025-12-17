# API 使用说明

## 重要提示

**Unlock 接口必须包含 `type` 字段！**

## Lock 接口

### 请求格式

```bash
POST /lock
Content-Type: application/json

{
  "type": "pull",           # 必需：操作类型 (pull/update/delete)
  "resource_id": "sha256:xxx",  # 必需：资源ID（镜像层digest）
  "node_id": "NODEA"        # 必需：节点ID
}
```

### 响应格式

```json
{
  "acquired": true,         # 是否获得锁
  "skip": false,            # 是否跳过操作
  "message": "成功获得锁"    # 消息
}
```

### 示例

```bash
curl -X POST http://127.0.0.1:8080/lock \
  -H "Content-Type: application/json" \
  -d '{
    "type": "pull",
    "resource_id": "sha256:test123",
    "node_id": "NODEA"
  }'
```

## Unlock 接口

### 请求格式

```bash
POST /unlock
Content-Type: application/json

{
  "type": "pull",           # 必需：操作类型 (pull/update/delete)，必须与 lock 时一致
  "resource_id": "sha256:xxx",  # 必需：资源ID
  "node_id": "NODEA",       # 必需：节点ID
  "success": true,          # 可选：操作是否成功（默认 false）
  "error": "错误信息"        # 可选：错误信息
}
```

### ⚠️ 重要：`type` 字段是必需的！

**错误示例（会返回"缺少必要参数"）：**

```bash
# ❌ 错误：缺少 type 字段
curl -X POST http://127.0.0.1:8080/unlock \
  -H "Content-Type: application/json" \
  -d '{
    "resource_id": "sha256:test123",
    "node_id": "NODEA",
    "success": true
  }'
```

**正确示例：**

```bash
# ✅ 正确：包含 type 字段
curl -X POST http://127.0.0.1:8080/unlock \
  -H "Content-Type: application/json" \
  -d '{
    "type": "pull",
    "resource_id": "sha256:test123",
    "node_id": "NODEA",
    "success": true
  }'
```

### 响应格式

```json
{
  "released": true,         # 是否成功释放锁
  "message": "成功释放锁"   # 消息
}
```

### 完整示例

#### 成功场景

```bash
# 1. 获取锁
curl -X POST http://127.0.0.1:8080/lock \
  -H "Content-Type: application/json" \
  -d '{
    "type": "pull",
    "resource_id": "sha256:test123",
    "node_id": "NODEA"
  }'

# 响应: {"acquired":true,"message":"成功获得锁","skip":false}

# 2. 释放锁（成功）
curl -X POST http://127.0.0.1:8080/unlock \
  -H "Content-Type: application/json" \
  -d '{
    "type": "pull",
    "resource_id": "sha256:test123",
    "node_id": "NODEA",
    "success": true
  }'

# 响应: {"released":true,"message":"成功释放锁"}
```

#### 失败场景

```bash
# 1. 获取锁
curl -X POST http://127.0.0.1:8080/lock \
  -H "Content-Type: application/json" \
  -d '{
    "type": "pull",
    "resource_id": "sha256:test123",
    "node_id": "NODEA"
  }'

# 2. 释放锁（失败）
curl -X POST http://127.0.0.1:8080/unlock \
  -H "Content-Type: application/json" \
  -d '{
    "type": "pull",
    "resource_id": "sha256:test123",
    "node_id": "NODEA",
    "success": false,
    "error": "下载失败"
  }'
```

## 操作类型

支持的操作类型：

- `pull` - 拉取镜像层
- `update` - 更新镜像层
- `delete` - 删除镜像层

**注意：** Lock 和 Unlock 请求中的 `type` 必须一致！

## 常见错误

### 错误 1: 缺少必要参数

**错误信息：** `缺少必要参数`

**原因：** Unlock 请求中缺少 `type`、`resource_id` 或 `node_id` 字段

**解决方法：** 确保请求包含所有必需字段：

```json
{
  "type": "pull",        // ← 必需！
  "resource_id": "...",  // ← 必需！
  "node_id": "..."       // ← 必需！
}
```

### 错误 2: 释放锁失败

**错误信息：** `释放锁失败：锁不存在或不是锁的持有者`

**原因：** 
- 锁不存在（从未获取过）
- 不是锁的持有者（其他节点持有锁）
- `type` 或 `resource_id` 不匹配

**解决方法：** 
- 确保先调用 Lock 接口获取锁
- 确保使用相同的 `type`、`resource_id` 和 `node_id`

## 测试脚本

使用提供的测试脚本可以避免这些错误：

```bash
# 基础测试（包含正确的参数）
./test-lock.sh

# 并发测试
./test-concurrent.sh
```

