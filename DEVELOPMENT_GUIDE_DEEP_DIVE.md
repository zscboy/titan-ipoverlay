# Titan IP Overlay 全栈技术白皮书 (核心开发手册)

本手册专为接手 Titan IP Overlay 项目的核心开发人员编写。它不仅涵盖了代码逻辑，还深入解析了分布式链路设计的哲学、协议规范以及生产级别的性能优化策略。

---

## 一、 系统全景观 (System Topology)

Titan IP Overlay 是一个三平面分离的分布式系统。

### 1. 控制平面 (Control Plane: `/manager`)
*   **职责**: 全局资源仲裁、节点注册流水线、用户权限/流量配额下发。
*   **核心逻辑**: 
    *   通过 `server.api` 定义 RESTful 接口。
    *   直接操作 Redis，维护节点在线列表（SortSet）和用户路由配置。
    *   **关键函数**: `createUser` (预设流量限制与路由模式), `switchUserRouteNode` (触发全局路由切换)。

### 2. 接入平面 (Access Plane: `/ippop`)
*   **职责**: 入口侧流量捕获与智能分流。
*   **交互**: 向上连接浏览器 (SOCKS5), 向下维护数千个边缘隧道的 WebSocket 长连接。
*   **核心模块**:
    *   `socks5/`: 协议解析器。支持扩展的用户名格式（如 `user-session-xxx`）来实现会话粘性。
    *   `ws/tunmgr.go`: 隧道池管理器。实现了负载均衡和 RTT 测速采样。

### 3. 出口平面 (Exit Plane: `/client`)
*   **职责**: 流量落地。模拟真实用户 IP。
*   **特点**: 轻量化，支持在 Windows/Linux/Android 多端部署。
*   **关键机制**: **0-RTT 建连缓冲**，确保在 TCP 还没连上目标网站时，数据已经“在路上”。

---

## 二、 协议规范 (Protocol Specification)

### 1. 通信载体：WebSocket + ProtoBuf
双端通信通过 `pb.Message` 进行序列化（见 `ippop/ws/pb/message.proto`）。

| 消息类型 (MessageType) | 发起端 | 作用 |
| :--- | :--- | :--- |
| `COMMAND` | POP | 发送控制指令（如 Kick、速率调整） |
| `PROXY_SESSION_CREATE` | POP | 要求边缘节点去连接某个目标 IP:Port |
| `PROXY_SESSION_DATA` | 双端 | 承载加密后的原始网际流量负载 |
| `PROXY_SESSION_CLOSE` | 双端 | 销毁本地 Socket 和 Session 映射 |
| `PROXY_UDP_DATA` | 双端 | 承载 UDP 报文及目标地址 |

---

## 三、 核心链路状态机 (Session Lifecycle)

理解一条 YouTube 字幕请求的完整生命周期：

1.  **握手阶段 (`ippop/socks5`)**:
    *   `authenticate()`: 解析加密用户名，提取 `session_id`。
    *   `HandleUserAuth()`: 从 Redis 校验流量余额与过期时间。
2.  **调度阶段 (`ippop/ws/tunmgr.go`)**:
    *   `selectBestTunnel()`: 选出 RTT 最低且未过载的边缘节点隧道。
3.  **启动阶段 (`ippop/ws/tunnel.go`)**:
    *   `acceptSocks5TCPConnImproved()`: **关键！** POP 端生成 Session，异步发起 `CREATE` 指令，同时开始从浏览器 Read。
4.  **缓冲阶段 (`client/tunnel/tunnel.go`)**:
    *   `createProxySession()`: 边缘节点在后台拨号 (Dial)，同时前端通过 `writeQueue` 接收 POP 同步推来的 TLS Client Hello。
5.  **传输阶段 (`tcpproxy.go`)**:
    *   双端启动 `proxyConn()`。使用 `sync.Pool` 管理 32KB 的复用 Buffer，通过 `io.Copy` 原理进行高速透传。
6.  **监控阶段**:
    *   隧道侧 `onPong()` 实时采集 RTT 并通过 `model.SetNodeNetDelay` 回传 Redis 指标。

---

## 四、 Redis 数据字典 (Data Structure)

系统是典型的“内存网关 + Redis 配置中心”模式：

*   `titan:user:{name}`: 哈希表。存储剩余流量、限速参数、绑定的 NodeID。
*   `titan:node:online`: 集合。当前存活的所有边缘节点 ID。
*   `titan:node:free`: 有序集合。按上线时间排序的可选节点池。
*   `titan:node:zset`: 存储节点元数据（IP、版本、操作系统、最终采样延迟）。
*   `titan:traffic5min:{name}`: 流量统计哈希。用于绘制带宽曲线。

---

## 五、 深度优化详解 (The "Black Magic")

### 1. 0-RTT (Zero Round-Trip Time)
*   **痛点**: 传统代理需要等 Edge 连上 YouTube 返回 OK，POP 才发数据。
*   **优化**: POP 端直接“盲发”。数据包在 Edge 的拨号过程中就在队列中排队。
*   **效果**: TLS 握手成功率提升，延迟压低 30%-60%。

### 2. 非阻塞异步写入队列 (Async-Writer)
*   **痛点**: WebSocket 写入锁是全局竞争的。
*   **优化**: 在 `Tunnel` 中引入 `writeChan`。所有业务协程只需把包投进 Channel 即可，由唯一的 `writeLoop` 负责推送到物理网卡。
*   **函数**: `ippop/ws/tunnel.go` 中的 `writeAsync` 与 `writeLoop`。

### 3. Latency-Aware 负载均衡
*   **算法**: 基于 RTT 的滑动平均。
*   **逻辑**: 在 `tunmgr.go` 中，系统会动态避开那些 RTT > 800ms 的节点。即便是“自动分配”模式，也能确保给用户最优的体验。

---

## 六、 开发者自诊清单 (Troubleshooting)

如果你发现系统变慢或失败，请按此顺序排查日志：

1.  **搜索 `[NODE_状态]`**: 如果频繁出现，说明边缘节点所在的网络极其不稳定。
2.  **搜索 `[DIAG]`**: 重点看 YouTube 侧是否返回了 `Connection Refused`。
3.  **查看 `writeChan full`**: 如果出现此日志，说明单台 POP 的万兆网卡带宽已满，或者 CPU 核心已跑满，需要横向扩展 POP。
4.  **Pprof 检查**: 运行 `curl http://localhost:6060/debug/pprof/goroutine?debug=1`。如果协程数持续增长，重点检查 `tcpproxy.go` 里的 `Read` 是否没有被正确 Close。

---
*编撰人: Antigravity (Advanced AI Engineer)*
*日期: 2025-12-26*
*适用版本: Titan Overlay v1.5+*
