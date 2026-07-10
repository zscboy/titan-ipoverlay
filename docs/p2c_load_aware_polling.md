# P2C-ε 负载感知就近 Polling 选盒

> polling 模式选出口盒子时，在"就近（低 RTT）"与"不打爆盒子/不牺牲 IP 利用率"之间做可调权衡。默认关闭，按账号灰度，改配置即回退。

## 1. 动机：为什么替换窗口方案

polling 现状是纯 FIFO 轮转（全局混播），忽略系统已测的 per-node RTT（`Tunnel.delay`，keepalive pong 回填）。本 PR 的前一版实现用"窗口 K 取 Top-M 随机"就近，但**修正语义的满载仿真证伪了它**（2000 盒、盒级并发容量中位 3、3400 rps、复刻 polling 共享轮转"选中不摘除"语义）：

| | 旧窗口方案 (K=8,M=2) | 纯轮转基线 |
|---|---|---|
| TTFB p50 | **2268ms** | 837ms |
| 1007 断连率 | **54.6%** | 9.7% |
| IP 利用率 | **43.5%**（1129 盒永久饥饿） | 100% |
| 最热盒峰值并发 | 33（容量 14×） | 8 |

根因：整窗按原序轮转 → 链表环序不变 → 窗口组成固化 → 非 top-M 盒永久零流量；且纯 RTT 偏好把并发堆到少数低容量住宅盒上 → 过载反而更慢。**利用率坍缩只是表象，打爆才是致命伤。**

## 2. 机制：竞速 + 保底 + 负载惩罚（三个正交旋钮）

`AcquireP2CPollingIP`（`ippop/ws/ippool.go`，持现有 `p.mu`，O(Depth)，零堆分配）：

```
n = len(pollSlice)                       # pollSlice 是 freeList 的伴生切片，O(1) 随机采样
n==0 → 返回空（不动计数器）
n < MinPool 或 R < 2 或 计数器 % R == 0    # 保底：取 LRU 队首（最饥饿盒）
      → acquirePollingFrontLocked()        # 与原混播逐字节一致
否则竞速：随机抽 Depth 个盒（有放回），每盒每 tunnel：
      delay ≤ 0（冷启动）/ lastPongAt 距今 >60s（陈旧）/ inflight ≥ MaxBoxSessions → 弃权
      score = delay + λ × inflight，取全场最低
      全弃权 → acquirePollingFrontLocked()  # 退化为混播，永不劣于原轮转
      否则 rotateEntryToBackLocked(胜者) 后返回
```

- **就近（Depth）**：min-of-D 随机竞速，最快盒被抽中概率 ≤ D/n，无固化赢家集合。
- **利用率（RREvery）**：每选中一次（两条路径）都 rotate 到队尾 → freeList 恒为 LRU、队首恒最饥饿；保底名额精确喂给它 → **任何盒子最多隔 池大小×R 次分配必被选中一次**（有界饥饿，构造性下界，单测 `TestP2C_FullCoverageWithinNxR` 断言）。
- **防打爆（λ）**：`inflight` 直接读现成 `tun.proxys.Count()`；快盒每快 λ ms 只允许多背 1 个在途会话 → 水位自限流。**消融证明**：λ=0 时满载 1007 从 8.8% 翻倍到 17.1%——负载项是就近安全的前提。

满载仿真（V3 参数 D=2/R=5/λ=100）：TTFB p50 −65ms、p95 −247ms、1007 8.8%（低于基线）、IP 利用率 100%、最热盒并发 9≈基线 8、最快盒份额硬上限 1.8× 公平份额。

## 3. 数据结构：pollSlice 伴生索引

`container/list` 无 O(1) 随机访问。加 `IPPool.pollSlice []*ipEntry` + `ipEntry.sliceIdx`，只在 `addToFreePool`（append）/`removeFromFreePool`（swap-remove）两个咽喉函数维护，各 3 行 O(1)、常开、16B/IP。切片只承担"成员集合+随机采样"，顺序（LRU）仍由三条链表持有。不变式（`len==freeList.Len()` + 双向指针互指）在 `TestP2C_PollSliceInvariant` 双向验证，`-race` churn 测试覆盖并发。

## 4. 配置与灰度

`ippop/config`，go-zero default 标签，全默认关：

| 参数 | 默认 | 说明 |
|---|---|---|
| `PollingP2CDepth` | 0 | D；0/1=关（与 master 逐字节一致），建议灰度 2，上限 4 |
| `PollingP2CRRInterval` | 5 | R；1=逐请求保底（等价原混播，安全滑轨），上限 10 |
| `PollingP2CLoadPenaltyMs` | 100 | λ；**<50 且 Depth≥2 → 强制关闭 + 告警**（纯 RTT 竞速会打爆快盒） |
| `PollingP2CMinPool` | 16 | 薄池护栏，低于此退化混播 |
| `PollingP2CMaxBoxSessions` | 0 | 单盒在途硬上限，0=关 |

`sanitizeP2CParams` 启动时校验一次并缓存（`TestSanitizeP2CParams` 钉住 λ=50 安全边界与各钳制值）。per-user：`model.User.P2CPolling`（redis hash，老记录缺字段=零值=关，向后兼容）；`PollingAllocator.Allocate` 仅当 `user.P2CPolling==1` 且全局开时走 P2C，否则原路径逐字节不变。

回退：改任一配置/清用户字段，不发版。

> **灰度启用前置缺口（已知，default-off 合并不受影响）**：`ModifyUser` RPC 当前不透传 `P2CPolling` 字段，而 userCache 是 LRU 无 TTL——开灰度前需给 `ModifyUserReq` 加该字段（proto + manager 透传），或运维走等效清缓存流程。

## 5. 护栏与可观测性

冷启动/陈旧 delay/薄池/超容均弃权退化，pollSlice 下标损坏时降级线性扫描 + 日志、绝不 panic 热路径。keepalive 统计日志新增：竞速/保底/退化三计数器（守恒：和 == 总分配数）、`pollSlice` vs `freeList` 一致性核对值。

## 6. 诚实边界

- **就近收益的性质**（HK 机实测，US 专属 POP vs 全球随机 POP，n=4000 同窗交错）：TTFB p50 1.99s vs 2.90s（−0.91s）、@3s 成功率 66.8% vs 35.0%（+31.8pt）——证实就近确有大提升，但是**紧超时段成功率收益（耐心曲线左移），不抬天花板**（@25s 最终成功率两 arm 相同、1007 无差）。
- **选盒层单项到不了 <600ms**：盒子 last-mile 基线与距离项同量级，理论上限 ~760-770ms；<600ms 需叠加供给侧就近（node→POP 绑定）+ 建链优化。
- **λ/MaxBoxSessions 是 per-POP 语义**：盒子挂多个 POP 时跨 POP 负载本 POP 不可见（靠 delay 陈旧弃权 + RTT 排队上升部分兜底）；满载仿真是单 POP 视角。
- 仿真绝对成功率偏乐观（未建模客户端主动 cancel 等），决策与验收以算法间相对差 + 实机 AB 为准。

## 7. 验证

`go build ./... && go vet ./ippop/ws/ && go test -race ./ippop/ws/`。灰度验收：HK 压测机同窗交错 AB（P2C 账号 vs 轮询账号），五指标齐报——TTFB p50/p95、蚂蚁口径成功率、1007、出口 IP 唯一率（≥75% 不回退）、单盒峰值并发；任一劣于基线即回滚。仿真器与结果见随附分析（非仓库内）。
