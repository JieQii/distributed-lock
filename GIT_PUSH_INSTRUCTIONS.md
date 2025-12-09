# Git推送操作说明

## 问题诊断

当前PowerShell无法识别`git`命令，可能的原因：
1. Git刚安装，需要重启PowerShell才能识别
2. Git未添加到系统PATH环境变量

## 解决方案

### 方案1：重启PowerShell（推荐）

1. **关闭当前的PowerShell窗口**
2. **重新打开PowerShell**
3. **导航到项目目录**：
   ```powershell
   cd C:\Users\WikiWiki\Desktop\code
   ```
4. **执行Git命令**（见下方命令列表）

### 方案2：使用Git Bash

如果安装了Git for Windows，可以使用Git Bash：

1. **右键点击项目文件夹** → 选择 "Git Bash Here"
2. **执行以下命令**：

```bash
git init
git add .
git commit -m "Initial commit: Distributed lock system with pull/update/delete operations and reference counting"
git branch -M main
git remote add origin https://github.com/JieQii/distributed-lock.git
git push -u origin main
```

### 方案3：使用Git GUI

1. **右键点击项目文件夹** → 选择 "Git GUI Here"
2. 在Git GUI中：
   - 点击 "Rescan" 扫描文件
   - 点击 "Stage Changed" 暂存所有文件
   - 输入提交信息
   - 点击 "Commit"
   - Repository → Push → 输入远程URL：`https://github.com/JieQii/distributed-lock.git`

### 方案4：手动添加Git到PATH

如果Git已安装但未在PATH中：

1. **找到Git安装路径**（通常在 `C:\Program Files\Git\cmd`）
2. **添加到系统PATH**：
   - 右键"此电脑" → 属性 → 高级系统设置 → 环境变量
   - 在"系统变量"中找到Path，点击编辑
   - 添加Git的cmd目录路径
   - 确定保存
3. **重启PowerShell**

## 完整的Git命令序列

在能够使用`git`命令后，执行以下命令：

```bash
# 1. 初始化仓库
git init

# 2. 添加所有文件
git add .

# 3. 提交代码
git commit -m "Initial commit: Distributed lock system with pull/update/delete operations and reference counting"

# 4. 设置主分支
git branch -M main

# 5. 添加远程仓库
git remote add origin https://github.com/JieQii/distributed-lock.git

# 6. 推送到GitHub
git push -u origin main
```

## 认证说明

推送时可能需要认证：

### 使用Personal Access Token（推荐）

1. 访问：https://github.com/settings/tokens
2. 点击 "Generate new token (classic)"
3. 选择 `repo` 权限
4. 生成token后，在推送时：
   - 用户名：你的GitHub用户名
   - 密码：使用生成的token

### 使用SSH（如果已配置）

```bash
git remote set-url origin git@github.com:JieQii/distributed-lock.git
git push -u origin main
```

## 验证推送成功

推送成功后，访问 https://github.com/JieQii/distributed-lock 应该能看到所有文件。

## 项目文件清单

以下文件将被推送到GitHub：

- ✅ `.gitignore` - Git忽略配置
- ✅ `go.mod` - Go模块定义
- ✅ `README.md` - 项目说明
- ✅ `server/` - 服务端代码和文档
- ✅ `client/` - 客户端代码和测试
- ✅ `content/` - Content插件集成
- ✅ 所有测试文件和文档

