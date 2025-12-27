# Titan IP Overlay 技术开发与快速上手指南

本手册旨在帮助新加入的开发人员快速理解 Titan IP Overlay 的系统架构、模块分工及核心业务逻辑。

---

## 一、 系统架构概览

Titan IP Overlay 是一个三层架构的分布式代理网络：

1.  **控制平面 (Manager)**: 全局大脑，负责资源分配、用户鉴权和 API 后台。
2.  **接入平面 (IPPop)**: 入口网关，负责承接用户代理协议（SOCKS5/HTTP）并调度至最优边缘。
3.  **出口平面 (Edge Client)**: 分布且匿名的出口节点，负责最终的流量落地。

**数据流向**: `用户浏览器` -> `IPPop (SOCKS5)` -> `WebSocket 隧道` -> `Edge Client` -> `目标网站 (YouTube/Google)`

---

## 二、 模块详细拆解

### 1. `/manager` (管理后台)
*   **定位**: 基于 `go-zero` 框架实现的 RESTful API 服务。
*   **核心逻辑**: 
    *   `internal/logic`: 包含用户创建、流量配额管理、Node 注册逻辑。
    *   `model`: 维护 Redis 中的全局状态（节点在线状态、用户流量包等）。
*   **上手重点**: 理解 `server.api` 文件，这是整个管理后台的定义文件。

### 2. `/ippop` (接入网关服务器)
*   **定位**: 高性能协议转发器。
*   **核心子模块**:
    *   `socks5/`: 完整的 SOCKS5 协议实现。重点看 `authenticate`（流量权限校验）和 `handleSocks5Connect`（建立连接请求）。
    *   `ws/`: **系统灵魂**。管理与成千上万个 Edge Client 的长连接（Tunnel）。
    *   `ws/tunmgr.go`: 隧道管理器。负责节点的选路算法（Latency-aware Scheduling）和心跳维护。
    *   `ws/tunnel.go`: 封装了 WebSocket 传输协议。实现了**异步写入队列**和**数据帧解包**。

### 3. `/client` (边缘节点客户端)
*   **定位**: 轻量级出口代理程序，通常部署在家庭宽带或边缘云上。
*   **核心子模块**:
    *   `tunnel/`: 接收来自 IPPop 的控制指令（创建会话、关闭会话、转发数据）。
    *   `tunnel/tcpproxy.go`: 实现了**预缓冲机制**（0-RTT），即在 TCP 建立过程中就开始接收并排队推送数据包。

---

## 三、 核心技术深度解析 (进阶必看)

### 1. 0-RTT 链路加速原理
为了极致的 TLS 握手速度，系统跳过了传统的“建连确认”往返。
*   **实现**: IPPop 发送 `PROXY_SESSION_CREATE` 后立即紧跟数据包；Client 端在 `net.Dial` 异步执行时，就通过 `writeQueue` 接收数据。
*   **代码位置**: `ippop/ws/tunnel.go` -> `acceptSocks5TCPConnImproved`。

### 2. 时延感知调度 (Latency-Aware)
*   **原理**: 系统通过 WebSocket 心跳包计算 RTT。
*   **选路逻辑**: 在 `tunmgr.go` 中，`selectBestTunnel` 会过滤出近期平均 RTT 最低的节点，并加入 20% 的随机方差防止节点过热。

### 3. 高并发异步 IO 架构
为了避免 WebSocket 写入锁（WriteLock）竞争，每个隧道都有一个独立协程处理写入：
```go
// tunnel.go 中的异步写入逻辑
func (t *Tunnel) writeAsync(msg []byte) error {
    select {
    case t.writeChan <- msg: 
        return nil
    default:
        return fmt.Errorf("buffer full") // 触发这一步说明节点处理能力达到极限
    }
}
```

---

## 四、 快速上手步骤

### 1. 环境准备
*   安装 Go 1.20 或以上版本。
*   准备一个 Redis 实例（系统强依赖 Redis 做节点状态同步）。

### 2. 本地跑通
1.  **启动 Manager**: 修改 `manager/etc/server.yaml` 中的 Redis 配置，运行 `go run manager/server.go`。
2.  **启动 IPPop**: 修改 `ippop/etc/server.yaml`，运行 `go run ippop/mian.go`。
3.  **启动 Client**: 获取 PoP 的 URL 和 Token，运行 `go run client/main.go`。

### 3. 诊断与调试
*   **日志标签**: 
    *   看到 `[NODE_不稳定]`：查网络。
    *   看到 `[DIAG]`：看协议栈报错。
*   **性能分析**: 代码集成了 `pprof`，访问 `:6060/debug/pprof` 即可查看内存和协程开销。

---

## 五、 后续开发建议 (Roadmap)

1.  **UDP 性能进一步优化**: 目前 UDP 基于缓存地址，未来可以考虑引入 KCP 协议减少丢包重传。
2.  **多 PoP 负载均衡**: 当单台 IPPop 机器连接数超过 5 万时，需要考虑在 API 层实现多 PoP 的动态分配。

---
*编撰人: Antigravity*
*日期: 2025-12-26*
