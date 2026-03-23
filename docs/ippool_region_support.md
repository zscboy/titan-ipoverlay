# IPPool 增加地区（国家）支持设计文档

## 1. 背景与目标
为了满足用户根据地理位置（国家/地区）选择出口 IP 的需求，需要对 `ippop/ws/ippool` 进行功能扩展。重构后的 `IPPool` 将支持：
- 按特定地区代码（如 "US", "CN", "HK"）快速分配 IP。
- 继续支持现有的全局不区分地区的 IP 分配。
- 保持高性能的 $O(1)$ 分配与释放效率。

## 2. 技术设计方案

### 2.1 地区属性来源：URL 参数上报
为了追求极致性能并降低依赖，地区信息由 Node 节点在建立 WebSocket 连接时通过 URL 参数主动上报。

- **URL 示例**: `ws://manager:port/node?id=node_1&region=US&os=linux`
- **解析层**: 在 `ippop/ws/nodews.go` 的 `NodeWSReq` 中增加 `Region string `form:"region,optional"`` 标签。
- **优点**: 避免了服务端查询本地 GeoIP 数据库的 CPU 和 IO 开销，且支持通过启动参数手动锁定节点区域。

### 2.2 核心数据结构：双索引空闲列表
为了同时兼顾“全局分配”和“区域分配”，系统将维护两套逻辑上的空闲列表索引。

#### `ipEntry` 结构体
在 `ippop/ws/ippool.go` 中，`ipEntry` 将保存两个在链表中的“位置存根”：
```go
type ipEntry struct {
    ip             string
    region         string        // 记录 IP 所属地区
    tunnels        map[string]*Tunnel
    globalElement  *list.Element // 指向其在全局 freeList 中的位置
    regionElement  *list.Element // 指向其在对应区域 freeList 中的位置
    assignedNodeID string
    isBlacklisted  bool
}
```

#### `IPPool` 结构体
`IPPool` 管理一个全局列表和一个区域映射表：
```go
type IPPool struct {
    mu sync.Mutex
    allIPs          map[string]*ipEntry
    freeList        *list.List                // 全局空闲列表
    regionFreeLists map[string]*list.List     // 键为 region 代码，值为该区域的空闲列表
    // ... 其他统计字段
}
```

### 2.3 同步逻辑控制
必须确保一个 IP 在全局分配后，在区域列表中同步消失，反之亦然。

- **Acquire (分配)**: 无论通过全局还是区域搜索找到 `ipEntry`，都必须调用 `list.Remove` 从**两个**列表中同时移除該 IP，并将 `globalElement` 和 `regionElement` 置为空。
- **Release (回收)**: 当 IP 空闲时，同时将其 `PushBack` 到 `freeList` 和对应的区域列表中，并更新 `ipEntry` 中的两个指针。
- **一致性**: 所有操作在 `IPPool.mu` 互斥锁保护下执行，确保原子性。

## 3. 性能影响评估

### 3.1 内存消耗 (10万级节点规模)
| 增加项 | 单个节点增量 | 10万节点总增量 |
| :--- | :--- | :--- |
| `ipEntry` 新增字段 | ~32 字节 | 3.2 MB |
| 额外的区域 `list.Element` | 40 字节 | 4.0 MB |
| **总计** | **约 72 字节** | **约 7.2 MB** |
**结论**: 内存消耗极低，对单机 TB/GB 级内存几乎无感。

### 3.2 CPU 性能
- **分配/回收时间**: 均为 $O(1)$ 指针操作，单次处理耗时增加在纳秒级，锁持有时间无显著差异。
- **查询优化**: 由于采用 URL 参数透传地区，服务端省去了 GeoIP 本地数据库毫秒级的查询开销，在高并发连接场景下，整体 CPU 负担呈下降趋势。

## 4. 实施路线图
1. **协议层**: 修改 `NodeWSReq` 解析逻辑。
2. **数据层**: 在 `IPPool` 初始化时创建 `regionFreeLists` 映射。
3. **分配层**: 
    - 封装私有同步函数处理双向列表的进出。
    - 在 `AcquireIP` 中增加可选的 `region` 参数。
4. **调用层**: 在 `TunnelManager.HandleSocks5TCP` 中，从 SOCKS5 认证后的 `User` 信息中提取 `region` 标签，并透传给分配器。

## 5. 异常处理
- **地区不存在/无 IP**: 若用户指定的区域没有可用节点，系统将遵循降级策略（目前建议返回错误或分配默认区域，视业务需求而定）。
- **参数缺失**: 若 Node 未上报地区，系统默认归入 "Unknown" 区域，不影响全局列表的使用。
