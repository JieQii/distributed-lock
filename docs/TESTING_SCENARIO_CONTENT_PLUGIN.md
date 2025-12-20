## Content 插件联调测试说明（多节点 + 单实例 Server）

本说明用于在一台物理/虚拟机上，同时运行多套 containerd（模拟多节点 Content 插件），共用一个分布式锁 server，验证“下载镜像层场景下的仲裁模块”是否正常工作。

---

## 1. 测试目标

- **互斥仲裁**：同一镜像层在多个节点并发拉取时，分布式锁保证同一时刻只有一个节点持锁，其余排队或等待。
- **回调 + 本地计数**：Content 插件在本地通过 `callback` + 引用计数存储，决定是否需要执行操作（skip / 执行），server 只负责锁。
- **队列推进**：持锁节点释放（成功或失败）后，等待队列中的节点能被依次唤醒并获得锁。
- **连接异常处理**：持锁节点连接异常断开时，server 的 HTTP 监控逻辑可以检测并按失败路径释放锁，推进队列。
- **观测性**：server 与各节点 Content 插件日志清晰记录加锁、排队、解锁、监控介入等关键行为。

---

## 2. 环境与前提条件

- 一台测试机（Linux 或 Windows，以下命令以 Linux Bash 为例，Windows 可作适配）。
- 分布式锁 server 二进制：`lock-server`（已在兼容的工具链上编译好，测试机只负责运行）。
- Content 插件二进制：`content-plugin`（内部已集成 client 逻辑，通过 HTTP 调用 lock server 的 `/lock` 与 `/unlock`）。
- 同一台机器上运行多套 containerd，示例：
  - 节点 A：`/run/containerd-a/containerd.sock`，数据目录 `/var/lib/containerd-a`
  - 节点 B：`/run/containerd-b/containerd.sock`，数据目录 `/var/lib/containerd-b`
- 镜像仓库：可访问一个公共镜像（如 `docker.io/library/busybox:latest`），用于测试同一镜像层的并发拉取。

> 说明：测试机上的 Go 工具链版本可以不完全兼容，只要能运行构建机产出的二进制即可。构建与测试环境可分离。

---

## 3. 日志目录规划与重定向

建议统一使用单独的日志目录，便于后期排查：

- 分布式锁 server 日志：`/var/log/distributed-lock/server.log`
- containerd 日志：
  - 节点 A：`/var/log/containerd/containerd-a.log`
  - 节点 B：`/var/log/containerd/containerd-b.log`
- Content 插件日志：
  - 节点 A：`/var/log/content-plugin/plugin-a.log`
  - 节点 B：`/var/log/content-plugin/plugin-b.log`

示例创建目录：

```bash
mkdir -p /var/log/distributed-lock
mkdir -p /var/log/containerd
mkdir -p /var/log/content-plugin
```

---

## 4. 启动分布式锁 Server（单实例）

```bash
./lock-server \
  --addr 0.0.0.0:8080 \
  > /var/log/distributed-lock/server.log 2>&1 &
```

说明：
- `--addr`：监听地址与端口，可按需修改。
- `> /var/log/distributed-lock/server.log 2>&1`：将标准输出与错误重定向到日志文件。
- `&`：后台运行。

Windows PowerShell 大致等价示例（仅供参考）：

```powershell
mkdir C:\logs\distributed-lock -Force

.\lock-server.exe --addr 0.0.0.0:8080 `
  > C:\logs\distributed-lock\server.log 2>&1 &
```

---

## 5. 启动多套 containerd（模拟多节点）

以 Linux 为例，启动两套 containerd 实例 A/B：

```bash
# 节点 A
mkdir -p /run/containerd-a /var/lib/containerd-a
containerd \
  --address /run/containerd-a/containerd.sock \
  --root /var/lib/containerd-a \
  --state /run/containerd-a \
  > /var/log/containerd/containerd-a.log 2>&1 &

# 节点 B
mkdir -p /run/containerd-b /var/lib/containerd-b
containerd \
  --address /run/containerd-b/containerd.sock \
  --root /var/lib/containerd-b \
  --state /run/containerd-b \
  > /var/log/containerd/containerd-b.log 2>&1 &
```

---

## 6. 启动多节点 Content 插件

假设 Content 插件二进制为 `content-plugin`，通过环境变量配置：

- `LOCK_SERVER_URL`：锁 server 的 HTTP 地址（如 `http://127.0.0.1:8080`）
- `NODE_ID`：节点 ID，用于区分不同节点在引用计数与日志中的标识。
- `CONTENTD_SOCKET`：当前插件所绑定的 containerd socket。

示例：

```bash
LOCK_SERVER_URL=http://127.0.0.1:8080

# 节点 A 插件
NODE_ID=node-a
CONTENTD_SOCKET=/run/containerd-a/containerd.sock

LOCK_SERVER_URL=$LOCK_SERVER_URL \
NODE_ID=$NODE_ID \
CONTENTD_SOCKET=$CONTENTD_SOCKET \
./content-plugin \
  > /var/log/content-plugin/plugin-a.log 2>&1 &

# 节点 B 插件
NODE_ID=node-b
CONTENTD_SOCKET=/run/containerd-b/containerd.sock

LOCK_SERVER_URL=$LOCK_SERVER_URL \
NODE_ID=$NODE_ID \
CONTENTD_SOCKET=$CONTENTD_SOCKET \
./content-plugin \
  > /var/log/content-plugin/plugin-b.log 2>&1 &
```

> 实际参数名称以 Content 插件实现为准；如采用 `config.toml` 或 containerd 本身的插件配置，也可以在配置中指定日志输出路径。

---

## 7. 测试场景与命令

### 场景 1：串行拉取（基线验证）

- 目的：验证单节点使用锁基本不出问题，以及串行情况下无排队。

**步骤**

1. 在节点 A 上拉取镜像：

   ```bash
   ctr --address /run/containerd-a/containerd.sock \
       images pull docker.io/library/busybox:latest
   ```

2. 等节点 A 完成后，在节点 B 上拉取同一镜像：

   ```bash
   ctr --address /run/containerd-b/containerd.sock \
       images pull docker.io/library/busybox:latest
   ```

**期望结果**

- server 日志中依次看到两个加锁/解锁流程，无明显排队日志。
- 两个数据目录下均能看到镜像存在：
  - `/var/lib/containerd-a` 下 busybox 可用；
  - `/var/lib/containerd-b` 下 busybox 可用。

---

### 场景 2：并发同层拉取（互斥与队列）

- 目的：验证同一资源在多节点并发请求时，只有一个节点持锁，其余排队/等待。

**步骤**

在两个终端中几乎同时执行：

```bash
# 终端 1：节点 A
ctr --address /run/containerd-a/containerd.sock \
    images pull docker.io/library/busybox:latest

# 终端 2：节点 B
ctr --address /run/containerd-b/containerd.sock \
    images pull docker.io/library/busybox:latest
```

或在同一终端中并发执行：

```bash
ctr --address /run/containerd-a/containerd.sock images pull docker.io/library/busybox:latest &
ctr --address /run/containerd-b/containerd.sock images pull docker.io/library/busybox:latest &
wait
```

**期望结果**

- server 日志中：
  - 对同一资源 key，只有一次 `acquired=true`；
  - 另一个节点请求被加入等待队列（queued），在持锁节点释放后获得锁。
- Content 插件日志中：
  - plugin-a / plugin-b 能反映哪个节点先获得锁、哪个处于等待状态。

---

### 场景 3：业务 skip（callback 前置决策）

- 目的：验证 Content 插件在本地通过 callback + 引用计数即可决定“不执行操作”，从而不请求分布式锁。

**准备**

- 在节点 B 的 Content 插件本地计数存储中，为目标镜像层设置状态，使 `ShouldSkipOperation` 返回 `skip=true`（具体方式取决于实现，可通过预置计数文件或测试开关）。

**步骤**

1. 节点 A / B 再次并发拉取同一镜像。
2. 观察 server 与插件日志。

**期望结果**

- server 日志中仅有节点 A（或未 skip 的节点）的锁请求记录；
- 节点 B 的插件日志中可以看到 `ShouldSkipOperation` 返回 `skip=true`，且未发起 `/lock` 请求。

---

### 场景 4：持锁失败释放并推进队列

- 目的：验证业务操作失败时，`/unlock` 能带着失败信息释放锁并唤醒排队节点。

**步骤**

1. 让节点 A 先获得该镜像层的锁（可通过先发起拉取，或者使用特定测试接口）。
2. 在 Content 插件中模拟下载失败（例如：在写入阶段主动返回错误）。
3. 确保插件仍然调用：

   - `Unlock(success=false, err=...)` → 对应 HTTP `/unlock` 携带失败信息。

4. 同时让节点 B 已经在等待队列中。

**期望结果**

- server 日志：看到失败路径的解锁日志，并且立即从等待队列中选出下一个请求分配锁。
- plugin-b 日志：在 A 失败释放后获得锁并继续操作。

---

### 场景 5：持锁节点连接异常（HTTP 监控）

- 目的：验证 server 的 HTTP 监控逻辑能在持锁连接异常断开时自动释放锁并推进队列。

**步骤**

1. 节点 A 获得锁并处于下载中。
2. 手动中断节点 A 与 server 的连接：
   - 如直接 kill 掉 Content 插件进程；
   - 或中断网络（测试环境中可用 iptables/防火墙模拟）。
3. 节点 B 已在等待队列中。

**期望结果**

- server 日志：
  - 监控 goroutine 检测到 HTTP 连接中断；
  - 按失败路径释放锁（等价于失败的 `/unlock`），执行 `delete lock -> processQueue`；
  - 之后分配锁给队列中的下一个请求（节点 B）。
- plugin-b 日志：在 A 断开后获得锁并继续执行。

---

## 8. 日志分析要点

- `server.log`：
  - `TryLock` 的输入/输出（acquired / queued / error）；
  - 队列操作：加入等待队列、从队列中取出队头；
  - `/unlock` 的处理路径（成功/失败）；
  - HTTP 监控检测到的连接中断及对应的锁释放行为。
- `plugin-a.log` / `plugin-b.log`：
  - `ShouldSkipOperation` 的决策和参数；
  - `UpdateRefCount` 的更新结果；
  - 拉取操作成功或失败日志；
  - 调用 `Lock` / `Unlock` 的时序与参数。

---

## 9. 构建与运行的推荐方式（关于 server / client / Content 插件）

### 9.1 是否只编译 server，不管 client？

不推荐只单独编译 server 而完全不考虑 client。原因：

- 真正对接 Content 插件的是 **client 逻辑**（封装 `/lock` `/unlock` 调用、重试、错误处理等）。
- Content 插件需要通过 client 与 lock server 通讯，若不编译 client，则插件侧要自己手写 HTTP 调用，容易与现有协议/行为不一致。

推荐做法是：

- **server**：单独编译成一个后台守护进程二进制（如 `lock-server`）。
- **client**：作为一个 Go 包（library）存在，被 Content 插件 `import` 使用，不单独出一个可执行文件。
- **Content 插件**：编译成一个独立二进制（如 `content-plugin`），**内部直接集成 client 包**，对外只暴露插件自身的入口。

### 9.2 是否要把 client 代码“合到” Content 插件里？

实现层面的建议：

- **代码结构层面**：保持 `client` 包独立存在（方便复用和测试），Content 插件通过 `import "distributed-lock/client"` 使用。
- **构建产物层面**：最终只发放一个 Content 插件二进制，其中已经静态/动态地包含了 client 的逻辑；不需要单独分发 `client` 可执行文件。

换句话说：

- 编译流程：
  - 构建机上：`go build -o lock-server ./server`
  - 构建机上：`go build -o content-plugin ./content`（其中 `./content` 依赖 `./client` 包）
- 部署到测试/生产：
  - 部署 `lock-server`（一个进程）
  - 部署 `content-plugin`（在每个 containerd 节点各跑一个进程，或按插件机制加载）
  - **不需要**单独部署“client 可执行文件”，client 只是 Content 插件内部使用的库。

这样既保持了架构清晰（server 只管锁，client 只管 HTTP 协议，Content 只管业务），又保证部署简单（每个角色一个二进制）。


