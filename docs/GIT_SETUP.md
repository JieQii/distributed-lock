# Git仓库设置说明

## 手动推送代码到GitHub

由于系统环境未检测到Git，请按照以下步骤手动操作：

### 1. 确保已安装Git

如果还没有安装Git，请从 https://git-scm.com/download/win 下载并安装。

### 2. 打开命令行（PowerShell或CMD）

在项目目录 `C:\Users\WikiWiki\Desktop\code` 中打开命令行。

### 3. 执行以下命令

```bash
# 初始化Git仓库
git init

# 添加所有文件
git add .

# 提交代码
git commit -m "Initial commit: Distributed lock system with pull/update/delete operations and reference counting"

# 设置主分支为main
git branch -M main

# 添加远程仓库
git remote add origin https://github.com/JieQii/distributed-lock.git

# 推送到GitHub（需要输入GitHub用户名和密码/Token）
git push -u origin main
```

### 4. 认证说明

如果使用HTTPS推送，GitHub现在要求使用Personal Access Token而不是密码：
1. 访问 https://github.com/settings/tokens
2. 生成新的token（选择 `repo` 权限）
3. 在推送时使用token作为密码

或者使用SSH方式：
```bash
# 使用SSH URL
git remote set-url origin git@github.com:JieQii/distributed-lock.git
git push -u origin main
```

## 项目文件清单

以下文件将被推送到GitHub：

### 核心代码
- `go.mod` - Go模块定义
- `README.md` - 项目说明文档
- `.gitignore` - Git忽略文件配置

### 服务端代码
- `server/main.go` - 服务端主程序
- `server/lock_manager.go` - 锁管理器（分段锁、引用计数）
- `server/handler.go` - HTTP请求处理器
- `server/types.go` - 类型定义
- `server/lock_manager_test.go` - 测试代码

### 客户端代码
- `client/client.go` - HTTP客户端实现
- `client/types.go` - 客户端类型定义
- `client/client_test.go` - 客户端测试

### Content插件集成
- `content/writer.go` - Writer实现
- `content/example.go` - 使用示例

### 文档
- `server/OPERATION_TYPES.md` - 操作类型说明
- `server/LOCK_OPTIMIZATION.md` - 锁优化说明
- `server/FLOWCHART.md` - 流程图
- `server/ARBITRATION_LOGIC.md` - 仲裁逻辑说明
- `server/UPDATE_OPERATION_ANALYSIS.md` - Update操作分析

## 功能特性

1. **分段锁机制**：32个分段，提升并发度
2. **三种操作类型**：Pull、Update、Delete
3. **引用计数管理**：跟踪资源使用情况
4. **FIFO队列**：确保请求按顺序获得锁
5. **重试机制**：客户端自动重试网络错误
6. **完整的测试覆盖**：包括并发测试和边界条件测试

