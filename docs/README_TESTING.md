# 测试脚本使用说明

## 脚本文件

项目提供了两个测试运行脚本：

- **`run_tests.sh`** - Linux/Mac/Unix 系统使用
- **`run_tests.bat`** - Windows 系统使用

## Linux/Mac 使用说明

### 1. 设置执行权限

首次使用前，需要给脚本添加执行权限：

```bash
chmod +x run_tests.sh
```

### 2. 运行脚本

```bash
./run_tests.sh
```

或者使用bash直接运行：

```bash
bash run_tests.sh
```

### 3. 验证脚本

如果遇到权限问题，可以检查：

```bash
# 检查文件权限
ls -l run_tests.sh

# 应该显示类似：
# -rwxr-xr-x 1 user user 1234 run_tests.sh
```

如果显示没有执行权限（没有x），运行：

```bash
chmod +x run_tests.sh
```

## Windows 使用说明

直接双击 `run_tests.bat` 或在命令行运行：

```cmd
run_tests.bat
```

## 脚本功能

两个脚本提供相同的功能：

1. 运行所有测试
2. 运行服务端测试
3. 运行客户端测试
4. 运行测试（详细输出）
5. 运行特定测试用例
6. 查看测试覆盖率
7. 退出

## 故障排查

### Linux/Mac 常见问题

**问题1：权限被拒绝**
```
bash: ./run_tests.sh: Permission denied
```

**解决：**
```bash
chmod +x run_tests.sh
```

**问题2：找不到命令**
```
bash: ./run_tests.sh: /bin/bash^M: bad interpreter
```

**解决：** 这通常是Windows和Linux换行符不同导致的
```bash
# 安装dos2unix（如果没有）
sudo apt-get install dos2unix  # Ubuntu/Debian
sudo yum install dos2unix      # CentOS/RHEL

# 转换文件
dos2unix run_tests.sh
```

或者使用sed：
```bash
sed -i 's/\r$//' run_tests.sh
```

**问题3：Go命令未找到**
```
错误: 未找到Go命令，请先安装Go
```

**解决：** 确保Go已安装并在PATH中
```bash
# 检查Go是否安装
go version

# 如果未安装，访问 https://golang.org/dl/ 下载安装
```

## 直接使用Go命令（不依赖脚本）

如果脚本无法运行，可以直接使用Go命令：

```bash
# 运行所有测试
go test ./...

# 运行所有测试（详细输出）
go test -v ./...

# 运行服务端测试
go test -v ./server

# 运行客户端测试
go test -v ./client

# 运行特定测试
go test -v -run TestConcurrentPullOperations ./server
```

## 跨平台兼容性

脚本已针对以下平台测试：

- ✅ Linux (Ubuntu, CentOS, Debian等)
- ✅ macOS
- ✅ Windows (使用.bat文件)

## 快速参考

| 平台 | 脚本文件 | 运行命令 |
|------|---------|---------|
| Linux/Mac | run_tests.sh | `chmod +x run_tests.sh && ./run_tests.sh` |
| Windows | run_tests.bat | `run_tests.bat` 或双击运行 |

