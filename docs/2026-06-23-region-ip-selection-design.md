# 指定国家/地区 IP — 可行性方案（POP 本地内存 + 通用标签抽象）

> 分支：`feature/region-ip-selection`（基于最新 `master` = `ce233e1`）
> 日期：2026-06-23
> 状态：待开发审核

---

## 0. 可行性结论（先看这里）

**可行，且低风险。** 原因：

1. **区域选择内核在最新 master 上已经写好**——`IPPool.AcquireIP(region)` + `regionFreeList`（按区域分桶的空闲链表）已存在，只是没有数据喂进去。本方案主要是「接线」，不是「造轮子」。
2. **改动集中在 `ippop` 一个服务内**，**不改 `client`（IoT 节点）**、**不改 `manager`**。
3. **全程内存、连接热路径零新增 Redis、零 `manager` 依赖**——直接回应开发提出的 4 点规模/可用性担忧。
4. **为后续"业务标签"预留扩展点**：复用你们 `business-pack-routing` 分支已验证的 `AllocationCriteria` 抽象，新增标签是加法而非重构。

预计改动量：ippop 内约 8 个文件、200 行级别；新增 1 个本地 GeoIP 库依赖 + 1 个 `.mmdb` 数据文件。

---

## 1. 背景与目标

作为成熟的 IP 代理网络，需支持客户在 socks5 连接里**指定出口 IP 的国家/地区**（例如要"美国 IP"）。客户已可通过用户名 DSL 携带区域：`account-region-us-session-xxx-sessTime-5`，其中 `region` 段已被 `ippop/socks5/protocol.go:paserUsername` 解析。

目标：让"带 `region` 的 socks5 请求"被分配到**该区域的出口节点**；带不到则**明确报错**（不静默给别国 IP）。

---

## 2. 现状评估（最新 master）

| 环节 | 现状 | 结论 |
|---|---|---|
| 区域分配内核 | `IPPool.AcquireIP(region)` 已实现"region 优先从 `regionFreeList[region]` 取"；`AddTunnel` 按 `t.opts.Region` 自动分桶；`AcquirePollingIP` 也维护区域链表 | **已就绪**（`ippop/ws/ippool.go`） |
| 节点 region 来源 | `NodeWSReq` 仅 `id/os/version`；`acceptWebsocket` 从不给 `TunOptions.Region` 赋值 → `regionFreeList` 恒空 | **缺供给侧数据** |
| 请求侧 region | `SocksTargetInfo` 无 `Region` 字段；`AcquireExclusiveNode(ctx)` 内部硬编码 `AcquireIP("")` | **未接线** |
| 用户名解析 | `paserUsername` 已解析出 `User.region` | **已就绪** |
| 节点模型 | `ippop/model/node.go` 的 `Node` 无 region 字段 | 可选持久化 |
| 标签抽象 | `business-pack-routing` 分支已有 `AllocationCriteria{Region, BusinessPack}` + `AcquireIP(criteria, evaluator)` + 内存 `PackStatusMatrix` | **可直接搬用** |

---

## 3. 设计原则（回应开发的 4 点担忧）

1. **连接热路径零阻塞**：区域来源用 **POP 本地内存 GeoIP 库**，单次 `IP→国家` 查询是内存二分查找（亚微秒~微秒级），不读 Redis、不发网络请求。
2. **数据预加载进内存**：`.mmdb` 启动时加载（mmap）常驻内存；选择索引 `regionFreeList` 本就在内存。**per-node region 不落 Redis**（随时可由 IP + 本地库推出）。
3. **不依赖 manager**：区域在 POP 本地解析，`manager` 挂了不影响节点上线与区域打标，保持"manager 非关键"的现状。**不采用"manager 打标再下发"的方案。**
4. **标签可扩展**：用通用的 `AllocationCriteria` 承载选择条件；区分"硬分区标签"（region）与"软评分标签"（业务包），新增标签是加法。

---

## 4. 总体架构与数据流

```
[节点接入]  IoT 连 POP /ws/node?id=&os=&version=
   └─ acceptWebsocket(conn, req, nodeIP)
        ├─ region := geoip.LookupCountry(nodeIP)   // 本地 .mmdb，内存，微秒级
        ├─ TunOptions.Region = region
        └─ IPPool.AddTunnel(t)  → 自动进 regionFreeList[region]   // 现成逻辑

[客户请求]  socks5 用户名 account-region-us-session-..
   └─ HandleUserAuth → HandleSocks5TCP(targetInfo{Region:"us", ...})
        └─ allocator.Allocate(user, target)
             └─ AcquireExclusiveNode(ctx, AllocationCriteria{Region:"us"})
                  └─ IPPool.AcquireIP("us")
                       ├─ regionFreeList["us"] 非空 → 取一个   ✅
                       └─ 为空 → 返回空 → 上层明确报错          ❗（不回退别国）
```

要点：节点侧"打区域标签"和客户侧"按区域选节点"都在 POP 内存完成；`manager`、`client` 均不参与。

---

## 5. 详细改动清单（全部在 `ippop`）

### 5.1 新增：本地 GeoIP 解析（内存）
- 新增 `ippop/geoip/`（或 `ippop/ws/geoip.go`）：封装一个进程级单例。
  - 启动时 `geoip2.Open("<path>/GeoLite2-Country.mmdb")` 加载进内存。
  - `func LookupCountry(ip string) string`：返回小写 ISO 3166-1 alpha-2（如 `us`）；查不到/出错返回 `""`。
- 依赖：`github.com/oschwald/geoip2-golang`（或更轻量的 `oschwald/maxminddb-golang` + 自定义 country 结构）。
- 配置：`ippop/config` 增加 `GeoIP.DBPath`（`.mmdb` 路径）；为空时降级为"不解析区域"（功能关闭，不影响现网）。

### 5.2 节点接入：打区域标签
- `ippop/ws/nodews.go`：`acceptWebsocket` 在已拿到 `nodeIP` 处，`region := geoip.LookupCountry(nodeIP)`。
- `ippop/ws/tunmgr.go`：构造 `TunOptions{...}` 处增加 `Region: region`（`IPPool.AddTunnel` 已据此分桶，**IPPool 零改动**）。
- 失败降级：`LookupCountry` 返回 `""` → 节点进全局/线路池，但不进任何区域桶（只服务不带 region 的请求）。**绝不因 GeoIP 失败而阻断节点上线。**
- （可选）`ippop/model/node.go` 的 `Node` 加 `Region string redis:"region"`，并在**现有** `HandleNodeOnline` 的 HMSet 里顺带写入（不新增 Redis 往返；仅供 admin 查看，非必需）。

### 5.3 请求侧：把 region 透传到分配器
- `ippop/socks5/socks5.go`：`SocksTargetInfo` 增加 `Region string`；`handleSocks5Connect` 填 `Region: req.user.region`。
- `ippop/http/httpproxy.go`：`handleHTTP` / `handleHTTPS` 构造 `SocksTargetInfo` 时填 `Region: user.region`（socks5 与 http/https 共用同一条数据面，一处定义即覆盖三入口）。
- 引入通用选择条件（搬用 business-pack-routing）：
  - `ippop/ws/allocator.go`：新增 `type AllocationCriteria struct { Region string /* 预留 BusinessPack 等 */ }` 与 `criteriaFromTarget(target)`。
  - `NodeSource` 接口：`AcquireExclusiveNode(ctx)` → `AcquireExclusiveNode(ctx, criteria AllocationCriteria)`。
  - `ippop/ws/tunmgr_alloc.go`：实现内部由 `AcquireIP("")` 改为 `AcquireIP(criteria.Region)`。
  - `ippop/ws/allocator_session.go`：2 处调用（`Allocate` 内）改为传 `criteriaFromTarget(target)`。

> **作用范围**：region 只作用于 **Custom 路由模式**（按 session 独占分配，走 `AcquireExclusiveNode`）——也就是客户用用户名 DSL 的那条线。`Static`（Auto/Manual/Timed，固定 `RouteNodeID`）天然不涉及区域；`Polling`（`AcquirePollingIP`）本期不带 region（见 §10）。

### 5.4 错误处理：无该国 IP → 明确报错
- region **严格匹配**：当 `criteria.Region != ""` 且 `regionFreeList[region]` 为空时，`AcquireExclusiveNode` 返回明确错误（如 `no available IP in region "us"`），由 `HandleSocks5TCP` 透出 → 客户连接失败并带原因。
- 因为当前没有任何调用方传非空 region，**此严格语义对现网行为零影响**。

### 5.5 不改动的部分（重要）
- **`client`（IoT 节点）**：不改。区域由 POP 反查节点出口 IP 得到。
- **`manager`**：不改。区域不绕 manager，manager 仍非关键。
- **`IPPool` 内核**：不改（`regionFreeList` / `AcquireIP(region)` 已具备）。

---

## 6. 标签可扩展性设计（region 是第 1 个标签，业务标签是第 2 个）

把"标签"分两类对待，这是避免未来重构的关键：

| 类型 | 角色 | 实现 | 来源 | 匹配方式 |
|---|---|---|---|---|
| **硬分区标签**（region） | 索引维度，O(1) 分桶 | `regionFreeList` | 本地 GeoIP，静态 | 精确匹配，不命中则报错 |
| **软评分标签**（business-pack，未来） | 桶内打分/过滤 | `AcquireIP(criteria, evaluator)` + 内存 `PackStatusMatrix` | 主动探测 / 被动 `ReportResult`，动态 | 在 region 桶内择优 |

落地方式：本期就引入 `AllocationCriteria` 结构（仅含 `Region`）。未来加业务标签时：
- `AllocationCriteria` 加字段（如 `BusinessPack`）；
- 用户名 DSL 多解析一段（如 `pack-xxx`）；
- `AcquireIP` 增加 `evaluator` 入参在桶内择优（直接搬 `business-pack-routing` 的 `acquireBestFromListLocked` + `PackStatusMatrix`）。

→ **接口签名与调用链已为此预留，新增标签是加法、不动既有逻辑。** 规模化时，`PackStatusMatrix` 建议启动**批量预热进内存**，避免每个 `ip|pack` 首次现查 Redis（与"预加载内存"原则一致）。

---

## 7. 性能与规模分析（10 万 ~ 百万节点）

- **区域解析**：MaxMind `.mmdb` 单次查询为内存二分查找，约数百纳秒~数微秒。即便 10 万节点同时接入，聚合 CPU 开销 < 1 秒、且分散在各连接 goroutine，**不阻塞、不读 Redis、不发网络**。
- **per-node region 不落 Redis**：随时可由 `IP + 本地库`推出，连接热路径**新增 0 次 Redis 读写**。
- **选择索引**：`regionFreeList` 内存哈希 + 链表，选择 O(1)。
- **manager 解耦**：区域全程 POP 本地，manager 宕机不影响。

> 独立提醒（不在本期范围）：现网 `acceptWebsocket` 每节点已有 `GetNode`(读) + `HandleNodeOnline`(写) 的 Redis I/O，这才是 10 万级并发接入的**既有瓶颈**。本方案不加重它；其批量/管线化/异步化建议另立专项优化。

---

## 8. GeoIP 库：选型、加载、更新、精度、许可

- **库**：MaxMind `GeoLite2-Country.mmdb`（约 9MB，国家级）即可；读取用 `oschwald/geoip2-golang`。需要更高精度可换付费 `GeoIP2`，或 `IP2Location` / `db-ip`（接口同构）。
- **加载**：进程启动加载进内存（mmap），常驻；提供"热替换"接口可选（收到 SIGHUP 或定时检测文件 mtime 重载）。
- **更新**：国家级地理数据变化缓慢，**周/月级**更新 `.mmdb` 文件即可（随发布或单独分发）。
- **取值规范**：统一**小写 ISO-3166-1 alpha-2**（`us`/`jp`/`de`），与用户名 `region-XX` 取值对齐（与 Bright Data 等同口径）。
- **精度**：国家级约 95%(US)/80%(其他)；先做国家级足够，州/城市级后续再议。
- **许可**：GeoLite2 免费但需 license key、受 EULA 约束（商用有限制）；正式商用建议评估付费 GeoIP2 或等价商用库。

---

## 9. 风险与缓解

| 风险 | 缓解 |
|---|---|
| GeoIP 查询失败/库缺失 | 降级为 `region=""`，节点照常上线（只是不进区域桶）；配置 `GeoIP.DBPath` 为空即整体关闭该功能，零影响现网 |
| 节点出口 IP 变化导致区域过时 | 节点(重)连即按当前 IP 重新解析；IP 变化通常伴随断连重连，区域自动刷新 |
| region 取值不一致（大小写/格式） | 统一小写 ISO-2，在解析处归一化 |
| 某 POP 上无客户所需区域的节点 | 明确报错（已定）；运营上需保证"客户路由到的 POP 上有目标区域节点"（见开放问题） |
| `.mmdb` 精度/许可 | 见 §8；先国家级 + 评估商用库 |

---

## 10. 不在本期范围（YAGNI）

- 业务标签（`business-pack`）的实际接入（仅预留 `AllocationCriteria` 扩展点）。
- 州/省/城市/ASN/ZIP 级粒度。
- Polling 模式的区域过滤（`AcquirePollingIP` 暂不带 region；如需，按同一 `AllocationCriteria` 接线即可）。
- `acceptWebsocket` 既有 Redis I/O 的批量化优化。

---

## 11. 测试与灰度

- **单测**：`IPPool` 区域分桶/取出/回退；region 严格匹配为空时返回空；`paserUsername` region 解析；`AllocationCriteria` 透传。
- **集成**：两个不同国家的节点接入 → 指定 `region-us` 的 socks5 连接命中 US 出口；指定无节点的区域 → 明确报错；不带 region → 行为与现网一致。
- **灰度**：`GeoIP.DBPath` 默认空（功能关闭）→ 先在测试 POP 配库验证 → 再逐 POP 放开。回滚 = 清空 `DBPath`。

---

## 12. 待开发确认的开放问题

1. **`.mmdb` 文件随 POP 部署可接受吗？**（国家级仅约 9MB，周/月更新）若不接受第三方库，可改为自维护"IP 段→国家"表加载进内存（接口不变，仅数据来源不同）。
2. **节点出口 IP 的取得**：`acceptWebsocket` 现用 `X-Real-IP / X-Forwarded-For / RemoteAddr`，确认它就是节点的真实公网出口 IP（与对外代理出口一致）。
3. **节点在 POP 间的分布**：客户能否拿到某国 IP，取决于其连上的 POP 上是否有该国在线节点。需确认现有 `manager.RegionStrategy`（节点→PoP 分布）与"POP 内按区域选节点"不冲突（POP 是否聚合多国节点）。
4. 是否需要把 `Node.Region` 持久化到 Redis 供 admin 查看（默认不持久化，按需开启）。
