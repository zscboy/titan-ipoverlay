# Tunnel 节点带宽测试功能设计文档 (v2.0)

## 1. 目标
为基于 WebSocket 隧道的节点提供标准的带宽测试能力，支持管理员从后台发起对单个或多个节点的：
*   **下行带宽测试 (Download)**：Server -> Node
*   **上行带宽测试 (Upload)**：Node -> Server
*   **延迟测试 (Latency)**：RTT 测量

## 2. 核心原理
*   **复用连接**：直接使用现有的 WebSocket 控制/数据隧道，无需额外建立 TCP/UDP 连接。
*   **异步指令**：通过 Protobuf 控制信令启动/停止测试流程。
*   **零拷贝处理**：服务器端（ippop）在接收测试数据包时仅进行计数累加，不解析内容，不进入业务逻辑。

## 3. 协议扩展 (Protobuf)

在 `message.proto` 中定义专用消息类型：

```protobuf
enum MessageType {
    // ... 原有类型 (0-6)
    BANDWIDTH_TEST_REQ  = 7; // 控制指令 (START/STOP)
    BANDWIDTH_TEST_DATA = 8; // 测试负载数据 (垃圾包)
}

// 频率/控制请求
message BandwidthTestRequest {
    enum Action {
        START = 0;
        STOP  = 1;
    }
    enum Direction {
        DOWNLOAD = 0; // 下行 (Server -> Node)
        UPLOAD   = 1;   // 上行 (Node -> Server)
    }
    
    Action action = 1;
    Direction direction = 2;
    int32 duration = 3;    // 持续时间（秒）
    string session_id = 4; // 测试会话唯一标识
}

// 结果上报
message BandwidthTestResult {
    string session_id = 1;
    bool success = 2;
    uint64 bytes_transferred = 3;
    int32 duration_ms = 4;
    double mbps = 5;
    string error_msg = 6;
}
```

## 4. 详细流程设计

### 4.1 上行带宽测试 (Node -> Server)
此场景测试节点的“上传能力”，对 `ippop` 的接收性能挑战最大。

1.  **任务下发**：Manager 通过 RPC 通知 `ippop`。
2.  **指令传递**：`ippop` 发送 `BANDWIDTH_TEST_REQ(START, UPLOAD, Duration)` 给 Node。
3.  **高速打流**：
    *   Node 开启独立协程，以最大速度通过 WebSocket 发送 `BANDWIDTH_TEST_DATA`。
    *   Payload 建议固定为 **32KB** 的静态 Buffer（避免频繁申请内存）。
4.  **计数统计**：
    *   `ippop` 收到 `BANDWIDTH_TEST_DATA` 时，直接在 `Tunnel.onMessage` 中识别并累加 `atomic.Uint64` 计数器。
    *   **关键行为**：收到该类型包后，`ippop` 应立即返回，不进行任何解包或 Session 状态查找。
5.  **结束上报**：
    *   Node 计时结束，发送 `BANDWIDTH_TEST_REQ(STOP)` 给 Server。
    *   `ippop` 计算总流量与耗时，将 `BandwidthTestResult` 上报给 Manager。

### 4.2 下行带宽测试 (Server -> Node)
1.  **任务下发**：同上。
2.  **启动指令**：`ippop` 发送 `START` 给 Node，通知其准备接收。
3.  **Server 打流**：
    *   `ippop` 内部启动定时器和高频发送循环。
    *   **并发控制**：在打流期间，应暂时放开对该 Tunnel 的业务限速（RateLimiter）。
4.  **Client 统计**：Node 负责在本地累加接收字节。
5.  **结果回收**：`ippop` 发送 `STOP` 后，Node 计算结果并通过 `BandwidthTestResult` 回传给 Server。

## 5. 性能与安全保障

### 5.1 零拷贝与内存保护
*   **静态 Payload**：打流端预先填充一个 32KB 的数据块，循环发送。
*   **协议栈豁免**：`ippop` 端的 `readLimiter` 必须对 `BANDWIDTH_TEST_DATA` 类型的数据包进行逻辑跳过，以测得真实物理极限。

### 5.2 状态互斥
*   每个 `Tunnel` 实例内持有一个 `atomic.Bool` 类型的 `isTesting` 状态。
*   如果收到 `START` 指令时已在测试，直接丢弃新任务并返回 `Busy` 错误。

### 5.3 批量测试控制 (Manager 侧)
*   **并发度控制**：Manager 同时发起的测试任务数不应超过 `ippop` 处理能力的 10%，建议设置全局并发阈值（如同时测量 5 个节点）。

## 6. 管理接口设计

### 6.1 发起随机测试
```http
POST /api/v2/bandwidth/random-test
{
    "count": 10,         // 随机选 10 个在线节点
    "concurrency": 2,    // 每次同时并发测 2 个
    "type": "upload",
    "duration": 5
}
```

### 6.2 查询批次结果
```http
GET /api/v2/bandwidth/batch-report/{batch_id}
```

---
*文档版本：v2.0*
*日期：2026-03-06*
