# Titan IP Overlay

[English](#english) | [中文](#中文)

---

## English

Titan IP Overlay is a high-performance, distributed edge computing proxy network. It is designed to provide a "flat" network topology, enabling low-latency, high-concurrency access to target websites (such as YouTube) using a global network of edge nodes.

### 🏗 System Architecture

The project consists of three core components:

1.  **Manager (`/manager`)**: The control plane. Responsible for user management, node registration, traffic accounting, and global routing policy orchestration.
2.  **PoP Gateway (`/ippop`)**: The access plane. Serves as the entrance gateway for users (SOCKS5/HTTP). It intelligently schedules traffic to the most suitable edge nodes based on real-time network conditions.
3.  **Edge Node (`/client`)**: The exit plane. Lightweight nodes (residential or edge servers) that build secure WebSocket tunnels back to the PoP and execute terminal requests.

### 🌟 Key Technical Features

-   **Intelligent Routing Modes**: Supports multiple routing strategies including Auto-allocation, Manual selection, Timed switching, and Session-based custom routing.
-   **0-RTT Accelerator**: Implements an asynchronous session establishment protocol. Data transmission (e.g., TLS Client Hello) begins immediately without waiting for the terminal connection confirmation, drastically reducing TTFB.
-   **High-Concurrency WebSocket Tunneling**: Features a non-blocking asynchronous writer and prioritized task scheduling to handle thousands of concurrent requests without lock contention.
-   **Latency-Aware Scheduling**: A real-time RTT measurement subsystem that ensures traffic is always routed through nodes with the lowest vibration and latency.
-   **Traffic & Bandwidth Control**: Granular control over user traffic quotas and burst bandwidth limits (Download/Upload).
-   **Carrier-Grade Observability**: Built-in diagnostics (`[NODE_不稳定]`, `[BROWSER_INFO]`) to quickly isolate issues between ISP stability and application logic.

### 🚀 Getting Started

#### Prerequisites
- Go 1.20+
- Redis (for state management and stats)

#### Build
```bash
# Build PoP Gateway
go build -o ippop_server ./ippop/mian.go

# Build Edge Client
go build -o edge_client ./client/main.go

# Build Manager
go build -o mgmt_server ./manager/server.go
```

---

## 中文

Titan IP Overlay 是一个基于边缘计算的高性能分布式代理网络系统。该项目旨在通过全球分布的边缘节点，提供“扁平化”的网络拓扑结构，为用户提供低延迟、高并发的目标网站（如 YouTube）访问体验。

### 🏗 系统架构

项目由三个核心模块组成：

1.  **管理中心 (`/manager`)**: 控制平面。负责用户权限、节点注册、流量计费以及全局路由策略的编排。
2.  **PoP 接入网关 (`/ippop`)**: 接入平面。作为用户的入口网关（支持 SOCKS5/HTTP），根据实时网络质量，辅助用户将流量调度至最优边缘节点。
3.  **边缘节点 (`/client`)**: 出口平面。部署在家庭宽带或边缘服务器上的轻量化程序，通过 WebSocket 隧道连接 PoP，并执行最终的出口请求。

### 🌟 核心技术亮点

-   **多维度路由模式**: 支持“自动分配”、“手动指定”、“定时切换”以及“基于 Session 的自定义选路”，满足不同业务场景需求。
-   **0-RTT 链路加速**: 采用异步会话建立协议。在终端连接建立过程中同步启动数据传输（如 TLS Client Hello），极大压缩了首次响应时间（TTFB）。
-   **高并发隧道架构**: 基于非阻塞异步写入队列和优先级任务调度，支持在单隧道内处理数千路并发请求，消除锁竞争。
-   **时延感知调度算法**: 内置实时 RTT 监测子系统，确保流量始终避开抖动较高的节点，选择延迟最低的出口。
-   **精细化流控**: 提供颗粒度至用户的流量配额管理及带宽上下限（Upload/Download）控制。
-   **工业级可观测性**: 深度集成诊断标签（如 `[NODE_不稳定]`、`[BROWSER_INFO]`），秒级定位链路瓶颈是在 ISP 网络、出口节点还是应用逻辑。

### 🚀 快速开始

#### 环境要求
- Go 1.20+
- Redis (用于状态管理和统计)

#### 编译
```bash
# 编译 PoP 网关
go build -o ippop_server ./ippop/mian.go

# 编译边缘节点
go build -o edge_client ./client/main.go

# 编译管理后台
go build -o mgmt_server ./manager/server.go
```
