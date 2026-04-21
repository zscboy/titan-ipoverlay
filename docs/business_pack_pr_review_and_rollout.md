# PR 审核说明与上线说明

**适用 PR**: `feat(stats): add business-pack-aware routing and IP tag matrix`  
**目标分支**: `stats`  
**适用仓库**: `titan-ipoverlay`

---

## 1. 这次改动解决什么问题

`stats` 分支原有能力更偏向：

- 单维度路由
- POP 维度容量与分配
- 节点黑名单/基础 QoS
- 会话统计与性能观察

但它还缺少一层关键能力：

`同一个出口 IP 对不同业务站点的可用性是不同的，调度时不能只看“节点在线/离线”或“是否黑名单”，还需要看它是否适合当前业务包。`

这次改动补的就是这一层：

1. 请求进入时，先识别它属于哪个业务包
2. 调度时，按 `region + business pack` 选择更合适的出口 IP
3. 运行结果会被动回写到 Redis 业务包状态矩阵
4. manager 侧可以查询：
   - 某个 host 会被归到哪个业务包
   - 某个出口 IP 当前在各业务包下的标签状态

---

## 2. 核心改动概览

### 2.1 新增业务包分类器

新增目录：

- [ippop/businesspack/classifier.go](/Users/yuan/Documents/xiaomi下载IP质量筛选/titan-ipoverlay/ippop/businesspack/classifier.go)

职责：

- 根据显式 `pack` 或目标 host，把请求归入：
  - `youtube_streaming`
  - `code_download`
  - `image_cdn`
  - `commerce_web`
  - `social_media`
  - `research_news`
  - `general_web`

审核重点：

- 默认规则是否符合当前业务预期
- `requestedPack` 是否应该永远优先于自动识别
- `general_web` 兜底是否满足现网需要

### 2.2 新增 Redis 业务包状态矩阵

新增文件：

- [ippop/model/ip_pack_status.go](/Users/yuan/Documents/xiaomi下载IP质量筛选/titan-ipoverlay/ippop/model/ip_pack_status.go)
- [ippop/ws/pack_status_matrix.go](/Users/yuan/Documents/xiaomi下载IP质量筛选/titan-ipoverlay/ippop/ws/pack_status_matrix.go)

新增 Redis Key：

- `titan:node:packstatus:{ip}`

结构：

- field = `pack`
- value = JSON:
  - `label`
  - `success_count`
  - `failure_count`
  - `last_updated`

标签语义：

- `allow`
- `gray`
- `deny`
- `unknown`

审核重点：

- 状态衍生规则是否过于激进
- 被动学习是否满足现阶段需求
- 是否需要 TTL / 过期机制

### 2.3 调度链路接入业务包

核心文件：

- [ippop/ws/tunmgr.go](/Users/yuan/Documents/xiaomi下载IP质量筛选/titan-ipoverlay/ippop/ws/tunmgr.go)
- [ippop/ws/allocator.go](/Users/yuan/Documents/xiaomi下载IP质量筛选/titan-ipoverlay/ippop/ws/allocator.go)
- [ippop/ws/allocator_session.go](/Users/yuan/Documents/xiaomi下载IP质量筛选/titan-ipoverlay/ippop/ws/allocator_session.go)
- [ippop/ws/allocator_polling.go](/Users/yuan/Documents/xiaomi下载IP质量筛选/titan-ipoverlay/ippop/ws/allocator_polling.go)
- [ippop/ws/tunmgr_alloc.go](/Users/yuan/Documents/xiaomi下载IP质量筛选/titan-ipoverlay/ippop/ws/tunmgr_alloc.go)
- [ippop/ws/ippool.go](/Users/yuan/Documents/xiaomi下载IP质量筛选/titan-ipoverlay/ippop/ws/ippool.go)

行为变化：

1. 请求进入 `HandleSocks5TCP`
2. 先识别业务包
3. 组装 `AllocationCriteria`
4. `custom` / `polling` 路由模式按 `region + business pack` 选 IP
5. 优先选择 `allow`
6. 再退到 `gray/unknown`
7. 跳过 `deny`

审核重点：

- 当前只对 IPPool 路由路径生效，是否符合预期
- `rotate` / `removeFromFreePool` 是否保持了原先 free list 语义
- 现有 `region` / line 平衡逻辑是否被破坏

### 2.4 会话统计扩展

核心文件：

- [ippop/ws/sessionperf.go](/Users/yuan/Documents/xiaomi下载IP质量筛选/titan-ipoverlay/ippop/ws/sessionperf.go)
- [ippop/ws/sessionperfcollector.go](/Users/yuan/Documents/xiaomi下载IP质量筛选/titan-ipoverlay/ippop/ws/sessionperfcollector.go)
- [ippop/ws/tcpproxy.go](/Users/yuan/Documents/xiaomi下载IP质量筛选/titan-ipoverlay/ippop/ws/tcpproxy.go)
- [ippop/ws/tunnel.go](/Users/yuan/Documents/xiaomi下载IP质量筛选/titan-ipoverlay/ippop/ws/tunnel.go)
- [ippop/ws/udpproxy.go](/Users/yuan/Documents/xiaomi下载IP质量筛选/titan-ipoverlay/ippop/ws/udpproxy.go)

新增统计字段：

- `business_pack`
- `exit_ip`

并在启动时补了 ClickHouse 的防御式 schema 扩展：

- `ADD COLUMN IF NOT EXISTS business_pack`
- `ADD COLUMN IF NOT EXISTS exit_ip`

审核重点：

- 历史表结构兼容性
- ClickHouse 用户权限是否允许 `ALTER TABLE`
- 新增列是否影响下游报表

### 2.5 manager 新增查询接口

新增接口：

- `GET /business-pack/classify`
- `GET /ip/pack/status`

核心文件：

- [manager/internal/handler/routes.go](/Users/yuan/Documents/xiaomi下载IP质量筛选/titan-ipoverlay/manager/internal/handler/routes.go)
- [manager/internal/logic/classifybusinesspacklogic.go](/Users/yuan/Documents/xiaomi下载IP质量筛选/titan-ipoverlay/manager/internal/logic/classifybusinesspacklogic.go)
- [manager/internal/logic/getippackstatuslogic.go](/Users/yuan/Documents/xiaomi下载IP质量筛选/titan-ipoverlay/manager/internal/logic/getippackstatuslogic.go)
- [manager/model/ip_pack_status.go](/Users/yuan/Documents/xiaomi下载IP质量筛选/titan-ipoverlay/manager/model/ip_pack_status.go)

用途：

- 运维验证某个域名会被分到哪个业务包
- 排查某个出口 IP 当前的业务包标签

审核重点：

- 接口是否应该放到 JWT 保护范围内
- 返回字段是否足够支撑运营/调度排查

### 2.6 HTTP 入口补齐 pack/region 透传

核心文件：

- [ippop/http/httpproxy.go](/Users/yuan/Documents/xiaomi下载IP质量筛选/titan-ipoverlay/ippop/http/httpproxy.go)
- [ippop/http/user.go](/Users/yuan/Documents/xiaomi下载IP质量筛选/titan-ipoverlay/ippop/http/user.go)
- [ippop/socks5/protocol.go](/Users/yuan/Documents/xiaomi下载IP质量筛选/titan-ipoverlay/ippop/socks5/protocol.go)
- [ippop/socks5/socks5.go](/Users/yuan/Documents/xiaomi下载IP质量筛选/titan-ipoverlay/ippop/socks5/socks5.go)

作用：

- 显式 `pack` 不只在 SOCKS5 用户名里可用，HTTP 代理也可透传

---

## 3. 建议审核顺序

建议 reviewer 按下面顺序看，会更容易理解：

1. 先看业务包分类器
   - [ippop/businesspack/classifier.go](/Users/yuan/Documents/xiaomi下载IP质量筛选/titan-ipoverlay/ippop/businesspack/classifier.go)

2. 再看运行时入口
   - [ippop/ws/tunmgr.go](/Users/yuan/Documents/xiaomi下载IP质量筛选/titan-ipoverlay/ippop/ws/tunmgr.go)

3. 再看 IP 分配逻辑
   - [ippop/ws/allocator.go](/Users/yuan/Documents/xiaomi下载IP质量筛选/titan-ipoverlay/ippop/ws/allocator.go)
   - [ippop/ws/ippool.go](/Users/yuan/Documents/xiaomi下载IP质量筛选/titan-ipoverlay/ippop/ws/ippool.go)

4. 再看 Redis 标签矩阵
   - [ippop/model/ip_pack_status.go](/Users/yuan/Documents/xiaomi下载IP质量筛选/titan-ipoverlay/ippop/model/ip_pack_status.go)
   - [ippop/ws/pack_status_matrix.go](/Users/yuan/Documents/xiaomi下载IP质量筛选/titan-ipoverlay/ippop/ws/pack_status_matrix.go)

5. 最后看 manager 侧接口
   - [manager/internal/handler/routes.go](/Users/yuan/Documents/xiaomi下载IP质量筛选/titan-ipoverlay/manager/internal/handler/routes.go)
   - [manager/internal/logic/classifybusinesspacklogic.go](/Users/yuan/Documents/xiaomi下载IP质量筛选/titan-ipoverlay/manager/internal/logic/classifybusinesspacklogic.go)
   - [manager/internal/logic/getippackstatuslogic.go](/Users/yuan/Documents/xiaomi下载IP质量筛选/titan-ipoverlay/manager/internal/logic/getippackstatuslogic.go)

---

## 4. 风险点与审核关注点

### 4.1 调度行为改变风险

风险：

- 原本能被拿到的 IP，现在可能因为 `deny` 被过滤掉
- 现网某些业务在规则不完整时可能更多落到 `general_web`

建议：

- 第一阶段只让标签影响排序
- 第二阶段再让 `deny` 强过滤

### 4.2 规则覆盖不完整风险

风险：

- 某些站点 host 还没纳入默认规则

建议：

- 先通过 manager 的 `/business-pack/classify` 做校验
- 上线初期记录未命中 host，按周补规则

### 4.3 ClickHouse schema 兼容性风险

风险：

- 生产 ClickHouse 账号若没有 `ALTER TABLE` 权限，collector 启动时会报错

建议：

- 上线前确认权限
- 若权限不足，先手工执行加列

### 4.4 Redis 标签矩阵冷热启动风险

风险：

- 刚上线时大多数 IP 可能还是 `unknown`

建议：

- 初期允许 `unknown` 参与分配
- 等主动检测工具接入后再逐步强化约束

---

## 5. 上线说明（建议 SOP）

### 5.1 上线前准备

上线前确认：

1. Redis 可写
2. ClickHouse 可连通
3. ClickHouse 用户具备建表/加列权限，或已提前手工迁移
4. manager 与 ippop 的配置文件支持新增 `BusinessPack` 段

建议在配置中预留：

```yaml
BusinessPack:
  default_pack: general_web
  rules: []
```

即使先不加自定义规则，也能走内置默认规则。

### 5.2 建议上线顺序

#### 第一步：先发 manager

原因：

- manager 侧只是查询与配置能力，风险低
- 便于上线后先验证分类和标签可见性

验证：

- `GET /business-pack/classify?host=github.com`
- `GET /ip/pack/status?ip=<exit_ip>`

#### 第二步：再发 ippop

原因：

- ippop 才是真正改变分配行为的地方

验证：

- 新请求日志里出现 `pack` 和 `region`
- 会话统计里出现 `business_pack` / `exit_ip`
- Redis 中开始出现 `titan:node:packstatus:{ip}`

### 5.3 上线后验证项

#### 运行态

检查日志：

- `HandleSocks5TCP` 是否打印出 `pack`
- 分配是否正常
- 是否出现大量 `no_node_available`

#### Redis

检查：

- `HGETALL titan:node:packstatus:{ip}`

预期：

- 能看到按 pack 维度的 JSON 状态

#### manager 接口

检查：

- 域名分类是否符合预期
- 某个测试 IP 是否能查出状态

#### ClickHouse

检查：

- `session_perf` 是否存在新列
- 新写入记录里是否带 `business_pack` / `exit_ip`

### 5.4 建议灰度策略

建议不要一上来就对全量流量强启业务包约束。

推荐分 3 阶段：

1. 第 1 阶段：只上线识别、记录、查询
2. 第 2 阶段：让标签只影响排序
3. 第 3 阶段：再逐步让 `deny` 不参与分配

---

## 6. 回滚策略

### 6.1 代码回滚

如果上线后发现分配异常：

- 直接回滚到 `stats` 基线版本即可

因为这次改动不涉及 Redis 结构性破坏，不会阻止回滚。

### 6.2 配置降级

如果不想立刻回滚代码，也可以通过配置降级：

- 把 `BusinessPack.rules` 留空
- 默认只走 `general_web`

这样系统仍能运行，但业务包精细化能力基本失效，风险会更低。

### 6.3 数据侧处理

Redis 中新增的：

- `titan:node:packstatus:{ip}`

即使保留，也不会影响旧代码运行。  
如需清理，可后续离线删除，不建议上线事故时优先做这个动作。

---

## 7. 已完成验证

本次提交已执行：

```bash
go test ./ippop/businesspack ./ippop/socks5
go test ./ippop/model -run TestDeriveIPPackLabel
go test ./ippop/ws -run TestDoesNotExist
go test ./manager/... -run TestDoesNotExist
go test ./ippop/http ./client/tunnel -run TestDoesNotExist
go test ./... -run TestDoesNotExist
```

说明：

- 全仓编译验证通过
- 但并未覆盖真实 Redis / ClickHouse / 生产流量环境
- 生产验证仍需要按上线 SOP 执行

---

## 8. 上线后下一步建议

这次 PR 解决的是：

- 业务包识别
- 按标签参与分配
- 被动学习回写
- manager 可查询

上线后建议优先继续做：

1. 主动检测工具结果自动回写 Redis / manager API
2. 业务包规则的可配置化管理
3. `unknown -> gray/allow/deny` 的更精细演进策略

等这三步补齐后，系统会从“有业务包能力”进一步进化为“完整的标签化调度平台”。
