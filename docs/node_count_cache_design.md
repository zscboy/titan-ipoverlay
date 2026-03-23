# NodeCountOfPops 缓存设计文档

## 1. 背景介绍

在当前的 `titan-ipoverlay` 管理端实现中，`GetNodePop` 和 `GetPops` 接口都会调用 `model.NodeCountOfPops` 函数。该函数通过 Redis 的 Pipeline 机制执行 `SCard` 命令，获取每个 PoP 节点当前维护的节点数量。

### 现状分析
- **调用次数频繁**：每当有新节点注册或现有节点请求接入点时，都会触发此逻辑。
- **Redis 压力**：虽然使用了 Pipeline，但大量的并发请求仍会导致 Redis 不必要的 CPU 消耗和网络 IO。
- **性能损耗**：获取节点数量是负载均衡决策的关键步骤，将其从同步调用改为内存读取可以显著降低请求延迟。

## 2. 方案建议：本地内存缓存 + 定期异步刷新

为了平衡性能和数据实时性，建议采用 **本地内存缓存 (In-Memory Cache)** 结合 **定期异步刷新 (Backgound Refresh)** 的方案。

### 方案核心思想
1. 在 `ServiceContext` 中维护一个内存 Map，记录各 PoP 节点的连接数。
2. 启动一个后台协程，定时（如每 5-10 秒）从 Redis 批量获取最新的 SCard 结果并更新内存。
3. 业务逻辑 `selectPopFromList` 和 `GetPops` 绕过 Redis，直接从 `ServiceContext` 的内存 Map 中读取数据。

## 3. 详细设计

### 3.1 ServiceContext 扩展
在 `manager/internal/svc/servicecontext.go` 中增加缓存结构：

```go
type ServiceContext struct {
    // ... 原有字段
    NodeCountMap   sync.Map           // 格式为 map[string]int64
}
```

### 3.2 异步刷新逻辑
在 `NewServiceContext` 初始化时启动定时器：

```go
func (sc *ServiceContext) startNodeCountSync() {
    ticker := time.NewTicker(5 * time.Second) // 5秒刷新一次
    go func() {
        for range ticker.C {
            sc.refreshNodeCounts()
        }
    }()
}

func (sc *ServiceContext) refreshNodeCounts() {
    popIDs := make([]string, 0, len(sc.Pops))
    for id := range sc.Pops {
        popIDs = append(popIDs, id)
    }

    // 调用现有的 model.NodeCountOfPops
    counts, err := model.NodeCountOfPops(context.Background(), sc.Redis, popIDs)
    if err != nil {
        logx.Errorf("refreshNodeCounts error: %v", err)
        return
    }

    for id, count := range counts {
        sc.NodeCountMap.Store(id, count)
    }
}
```

### 3.3 业务逻辑调用改动（示意）

在 `selectPopFromList` 中，逻辑将简化为：

```go
func (l *GetNodePopLogic) selectPopFromList(ctx context.Context, popIds []string) (*svc.Pop, error) {
    // ... 原有判空逻辑
    
    // 改为从缓存读取
    nodeCountMap := make(map[string]int64)
    for _, id := range popIds {
        if val, ok := l.svcCtx.NodeCountMap.Load(id); ok {
            nodeCountMap[id] = val.(int64)
        } else {
            nodeCountMap[id] = 0 // 默认值
        }
    }

    // ... 后续根据 loadRatio 选择最优 PoP 的逻辑保持不变
}
```

## 4. 方案优缺点

### 优点
- **极速响应**：负载均衡计算完全在内存中完成，不再依赖网络调用。
- **降低负载**：显著减少 Redis 的 QPS，尤其是在 PoP 数量较多或节点接入峰值时。
- **隔离性好**：即使 Redis 短暂不可用，缓存中仍有数据可供决策，提升了系统的容错性。

### 缺点
- **数据滞后**：存在秒级的频率偏差，可能会导致瞬间分配过度（可通过缩短刷新间隔或在 `SetNodePopIP` 时手动预增缓存来缓解）。

## 5. 总结

`NodeCountOfPops` 非常适合做缓存。由于节点接入是一个低频变动但高频查询的操作，使用后台定期刷新的本地缓存是最佳实践方案。该方案实现简单，对现有架构改动极小，能有效提升 Manager 服务的吞吐量。
