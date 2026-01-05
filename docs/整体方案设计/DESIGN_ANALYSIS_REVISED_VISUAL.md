# 设计方案分析（修订版）- 可视化

> **场景**：8个节点，分布式容器环境，互联网用户，不能加全局锁

---

## 一、场景特点

```mermaid
mindmap
  root((实际场景))
    节点数量
      8个节点
      固定但可能动态变化
    环境
      分布式容器环境
      容器可能重启/扩缩容
    用户
      互联网用户
      需要高可用
    限制
      不能加全局锁
      不能有复杂锁机制
    业务规模
      4-5个镜像并发
      几十个层并发
```

---

## 二、当前方案问题（容器环境）

### 2.1 单点故障

```mermaid
graph TB
    subgraph Current["当前架构"]
        N[8个节点容器] -->|请求锁| S[锁服务端容器<br/>单点]
        S -->|故障| F[❌ 所有节点无法获取锁]
        F -->|影响| N
    end
    
    style S fill:#ff6b6b
    style F fill:#ff6b6b
```

**问题**：
- ❌ 锁服务端容器故障 → 系统完全不可用
- ❌ 容器重启 → 数据丢失
- ❌ 不符合互联网用户的高可用要求

### 2.2 易用性问题

```mermaid
graph LR
    A[互联网用户] --> B[部署需求]
    B --> C[需要部署锁服务端容器]
    B --> D[需要配置服务发现]
    B --> E[需要配置高可用]
    B --> F[需要配置持久化]
    
    C --> Problem[❌ 复杂度高]
    D --> Problem
    E --> Problem
    F --> Problem
    
    Problem --> Impact[易用性差<br/>不符合"不能加全局锁"]
    
    style Problem fill:#ff6b6b
    style Impact fill:#ff6b6b
```

---

## 三、新方案设计

### 3.1 方案A：配置驱动的一致性哈希（推荐）

```mermaid
graph TB
    subgraph Config["配置（ConfigMap）"]
        CN[节点列表<br/>node1-node8]
    end
    
    subgraph Nodes["8个节点容器"]
        N1[节点1<br/>本地计算]
        N2[节点2]
        N3[节点3]
        N4[节点4]
        N5[节点5]
        N6[节点6]
        N7[节点7]
        N8[节点8]
    end
    
    CN --> N1
    CN --> N2
    CN --> N3
    CN --> N4
    CN --> N5
    CN --> N6
    CN --> N7
    CN --> N8
    
    N1 -->|hash(resourceID) % 8| Check{是否分配给<br/>当前节点?}
    Check -->|是| Handle[处理资源]
    Check -->|否| Skip[跳过]
    
    style Check fill:#4ecdc4
    style Handle fill:#4ecdc4
```

**核心逻辑**：

```go
func ShouldHandle(resourceID string, nodeID string, nodeList []string) bool {
    hash := fnv.New32a()
    hash.Write([]byte(resourceID))
    index := hash.Sum32() % uint32(len(nodeList))
    return nodeList[index] == nodeID
}
```

**优点**：
- ✅ **无单点故障**：不需要锁服务端
- ✅ **简单可靠**：逻辑简单，易于理解
- ✅ **易用性好**：只需配置节点列表
- ✅ **无全局锁**：每个节点独立计算
- ✅ **适合容器环境**：配置通过ConfigMap管理

---

### 3.2 方案B：协调服务节点注册

```mermaid
graph TB
    subgraph Coordinator["协调服务"]
        ETCD[etcd/Consul<br/>节点注册]
    end
    
    subgraph Nodes["8个节点容器"]
        N1[节点1<br/>注册+监听]
        N2[节点2]
        N3[节点3]
    end
    
    N1 -->|注册| ETCD
    N2 -->|注册| ETCD
    N3 -->|注册| ETCD
    
    N1 -->|监听节点变化| ETCD
    N1 -->|计算分配| Assign[资源分配]
    
    style ETCD fill:#4ecdc4
    style Assign fill:#4ecdc4
```

**优点**：
- ✅ 自动节点发现
- ✅ 动态调整
- ✅ 高可用

**缺点**：
- ❌ 需要额外服务（etcd/Consul）
- ❌ 复杂度增加
- ❌ 易用性降低

---

### 3.3 方案C：Redis轻量级锁

```mermaid
graph TB
    subgraph Storage["共享存储"]
        Redis[Redis<br/>SETNX锁]
    end
    
    subgraph Nodes["8个节点容器"]
        N1[节点1<br/>SETNX获取锁]
        N2[节点2]
        N3[节点3]
    end
    
    N1 -->|SETNX| Redis
    N2 -->|SETNX| Redis
    N3 -->|SETNX| Redis
    
    Redis -->|成功| N1
    Redis -->|失败| N2[等待/重试]
    
    style Redis fill:#4ecdc4
```

**优点**：
- ✅ 成熟稳定
- ✅ 高可用（Redis Cluster）
- ✅ 轻量级

**缺点**：
- ❌ 需要额外服务（Redis）
- ❌ 仍有单点风险
- ❌ 易用性降低

---

## 四、方案对比

### 4.1 设计原则评分

```mermaid
graph LR
    subgraph Score["设计原则评分（20分）"]
        A[方案A<br/>配置驱动] --> A1[可靠性: ⭐⭐⭐⭐⭐]
        A --> A2[可定位性: ⭐⭐⭐⭐]
        A --> A3[易用性: ⭐⭐⭐⭐⭐]
        A --> A4[性能: ⭐⭐⭐⭐⭐]
        A --> A5[总分: 19/20]
        
        B[方案B<br/>协调服务] --> B1[可靠性: ⭐⭐⭐⭐]
        B --> B2[可定位性: ⭐⭐⭐⭐]
        B --> B3[易用性: ⭐⭐⭐]
        B --> B4[性能: ⭐⭐⭐⭐]
        B --> B5[总分: 15/20]
        
        C[方案C<br/>Redis锁] --> C1[可靠性: ⭐⭐⭐]
        C --> C2[可定位性: ⭐⭐⭐]
        C --> C3[易用性: ⭐⭐⭐]
        C --> C4[性能: ⭐⭐⭐⭐]
        C --> C5[总分: 13/20]
        
        D[当前方案<br/>锁服务端] --> D1[可靠性: ⭐⭐]
        D --> D2[可定位性: ⭐⭐⭐]
        D --> D3[易用性: ⭐⭐]
        D --> D4[性能: ⭐⭐⭐]
        D --> D5[总分: 10/20]
    end
    
    style A5 fill:#4ecdc4
    style B5 fill:#ffa500
    style C5 fill:#ffa500
    style D5 fill:#ff6b6b
```

### 4.2 容器环境适配

| 方案 | 容器重启 | 容器扩缩容 | 故障恢复 | 易用性 | 全局锁 |
|------|---------|-----------|---------|--------|--------|
| **方案A** | ✅ 自动 | ⚠️ 需更新配置 | ✅ 自动 | ⭐⭐⭐⭐⭐ | ✅ 无 |
| **方案B** | ✅ 自动 | ✅ 自动 | ✅ 自动 | ⭐⭐⭐ | ✅ 无 |
| **方案C** | ✅ 自动 | ✅ 自动 | ⚠️ 需Redis HA | ⭐⭐⭐ | ⚠️ 有 |
| **当前方案** | ❌ 数据丢失 | ❌ 需人工 | ❌ 需人工 | ⭐⭐ | ❌ 有 |

---

## 五、推荐方案：方案A

### 5.1 完整架构

```mermaid
graph TB
    subgraph K8s["Kubernetes集群"]
        subgraph Config["配置层"]
            CM[ConfigMap<br/>节点列表]
        end
        
        subgraph Pods["Pod层（8个节点）"]
            P1[Pod1<br/>node1]
            P2[Pod2<br/>node2]
            P3[Pod3<br/>node3]
            P4[Pod4<br/>node4]
            P5[Pod5<br/>node5]
            P6[Pod6<br/>node6]
            P7[Pod7<br/>node7]
            P8[Pod8<br/>node8]
        end
    end
    
    CM --> P1
    CM --> P2
    CM --> P3
    CM --> P4
    CM --> P5
    CM --> P6
    CM --> P7
    CM --> P8
    
    P1 -->|本地计算| Logic1[hash % 8 = 0?]
    P2 -->|本地计算| Logic2[hash % 8 = 1?]
    P3 -->|本地计算| Logic3[hash % 8 = 2?]
    
    Logic1 -->|是| Handle1[处理资源]
    Logic1 -->|否| Skip1[跳过]
    
    style CM fill:#4ecdc4
    style Logic1 fill:#4ecdc4
    style Handle1 fill:#4ecdc4
```

### 5.2 实现示例

```go
// 1. 从ConfigMap读取配置
func loadNodeList() []string {
    // 从环境变量读取（Kubernetes注入）
    nodesEnv := os.Getenv("NODE_LIST")
    var nodes []string
    json.Unmarshal([]byte(nodesEnv), &nodes)
    return nodes
}

// 2. 初始化ResourceAssigner
assigner := NewResourceAssigner(nodeID, nodeList)

// 3. 使用
if assigner.ShouldHandle(resourceID) {
    // 处理资源
} else {
    // 跳过，由其他节点处理
}
```

### 5.3 容器扩缩容处理

```mermaid
sequenceDiagram
    participant K8s as Kubernetes
    participant CM as ConfigMap
    participant C1 as 容器1-8
    participant C9 as 新容器9
    
    Note over K8s,C9: 扩容：添加节点9
    K8s->>CM: 更新ConfigMap<br/>添加node9
    K8s->>C9: 创建新容器
    C9->>CM: 读取配置
    C9->>C9: 初始化ResourceAssigner
    
    Note over K8s,C1: 现有容器重新加载配置
    K8s->>C1: 发送SIGHUP信号
    C1->>CM: 重新读取配置
    C1->>C1: UpdateNodeList
```

---

## 六、方案优势

### 6.1 方案A的优势

```mermaid
mindmap
  root((方案A优势))
    可靠性
      无单点故障
      无数据丢失风险
      自动故障恢复
    易用性
      只需配置节点列表
      无需额外服务
      适合容器环境
      符合"不能加全局锁"
    性能
      无锁竞争
      无网络请求
      本地计算
    可定位性
      问题容易追踪
      逻辑简单清晰
      易于调试
```

### 6.2 与当前方案对比

| 特性 | 当前方案 | 方案A |
|------|---------|-------|
| **单点故障** | ❌ 有 | ✅ 无 |
| **数据持久化** | ❌ 无 | ✅ 不需要 |
| **易用性** | ⭐⭐ | ⭐⭐⭐⭐⭐ |
| **复杂度** | ⭐⭐⭐⭐ | ⭐⭐ |
| **全局锁** | ❌ 有 | ✅ 无 |
| **容器适配** | ⚠️ 一般 | ✅ 优秀 |
| **互联网用户** | ❌ 不适合 | ✅ 适合 |

---

## 七、实施建议

### 7.1 迁移步骤

```mermaid
flowchart TD
    Start([开始迁移]) --> Step1[步骤1: 实现ResourceAssigner]
    Step1 --> Step2[步骤2: 集成到现有代码]
    Step2 --> Step3[步骤3: 配置管理（ConfigMap）]
    Step3 --> Step4[步骤4: 测试验证]
    Step4 --> Step5[步骤5: 逐步迁移]
    Step5 --> Step6[步骤6: 移除锁服务端]
    Step6 --> End([完成])
    
    style Step1 fill:#4ecdc4
    style Step6 fill:#4ecdc4
```

### 7.2 关键注意事项

1. **节点列表一致性**：
   - ✅ 所有节点必须使用相同的节点列表
   - ✅ 节点列表必须排序（保证一致性）

2. **容器扩缩容**：
   - ✅ 更新ConfigMap后，容器需要重新加载配置
   - ✅ 可以使用SIGHUP信号或Watch机制

3. **故障处理**：
   - ✅ 节点故障时，资源会重新分配给其他节点
   - ✅ 节点恢复后，资源分配会重新平衡

---

## 八、总结

### 8.1 核心结论

**对于8个节点的容器环境**：

1. **推荐方案A（配置驱动的一致性哈希）**：
   - ✅ 无单点故障
   - ✅ 简单可靠
   - ✅ 易用性好（只需配置）
   - ✅ 无全局锁
   - ✅ 适合容器环境
   - ✅ 适合互联网用户

2. **不推荐当前锁方案**：
   - ❌ 单点故障
   - ❌ 数据丢失风险
   - ❌ 易用性差
   - ❌ 不符合"不能加全局锁"的要求

### 8.2 关键原则

> **简单可靠 > 复杂高性能**

对于8个节点的容器环境，简单可靠的方案（方案A）是最佳选择。

