# Titan IP Overlay

[English](#english) | [中文](#中文)

---

## English

Titan IP Overlay is a high-performance, distributed proxy network based on edge computing. It provides a flat, low-latency link across global edge nodes.

### 🚀 Key Features (Performance Optimized)

- **0-RTT Link Establishment**: Optimized TLS handshake latency by initiating backend connections asynchronously and buffering initial data (Client Hello).
- **High Concurrency Architecture**: Implemented a non-blocking asynchronous writer for WebSocket tunnels to eliminate lock contention under high QPS.
- **Latency-Aware Scheduling**: Intelligent node selection based on real-time RTT measurements and variance analysis.
- **Resource Efficiency**: Integrated `sync.Pool` for 32KB buffer management to reduce GC pressure during small file transfers (e.g., YouTube subtitles).
- **Diagnostic-Ready**: Built-in diagnostic tags (`[NODE_不稳定]`, `[NODE_时延大]`) to distinguish between network instability and code issues.

### 🛠 Technology Stack

- **Languange**: Golang
- **Framework**: [go-zero](https://github.com/zeromicro/go-zero)
- **Log/Stats**: logx, pprof
- **Communications**: WebSocket (ProtoBuf)

---

## 中文

Titan IP Overlay 是一个基于边缘计算的高性能分布式代理网络系统。它通过全球边缘节点提供扁平化、低延迟的网络链路。

### 🚀 核心特性 (性能优化版)

- **0-RTT 链路建立**: 通过异步发起后端连接并预缓冲初始数据 (Client Hello)，显著降低 TLS 握手延迟。
- **高并发架构**: 为 WebSocket 隧道实现了非阻塞异步写入队列，消除了高 QPS 下的锁竞争瓶颈。
- **时延感知调度**: 基于实时 RTT 测量和方差分析的智能节点选择算法。
- **高效资源利用**: 集成 `sync.Pool` 管理 32KB 复用缓冲区，降低小文件请求（如 YouTube 字幕）时的 GC 压力。
- **深度诊断日志**: 内置诊断标签 (`[NODE_不稳定]`, `[NODE_时延大]`)，快速区分网络波动与代码异常。

### 🛠 技术栈

- **开发语言**: Golang
- **核心框架**: [go-zero](https://github.com/zeromicro/go-zero)
- **日志/监控**: logx, pprof
- **通信协议**: WebSocket (ProtoBuf 序列化)
