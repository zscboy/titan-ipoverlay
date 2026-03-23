# Tunnel 设备带宽测试功能设计文档 (v2.0)

## 1. 目标
为 Tunnel 设备（IoT 客户端）添加一个命令，用于测试其与 Server 端（IPPop）之间的网络带宽（上传/下载速度）和延迟。

## 2. 核心原理
利用现有的 WebSocket 隧道连接，通过发送特定类型的"负载数据包"来填充带宽，并计算传输速率。

*   **复用连接**：不建立新的 TCP/UDP 连接，直接复用现有的 WebSocket Tunnel。
*   **Protobuf 协议**：扩展现有的 `message.proto` 协议，增加带宽测试专用的消息类型。
*   **测试流程**：基于 **Start -> Stream -> Stop** 的三阶段模式。

## 3. 详细协议设计

我们在 `ippop/ws/proto/message.proto` 中扩展 `MessageType` 和定义新的消息结构。

### 3.1 消息类型 (MessageType)
```protobuf
enum MessageType {
    // ... 原有类型 ...
    
    // 带宽测试控制信令：用于开始、停止、汇报结果
    BANDWIDTH_TEST_REQ = 7; 
    
    // 带宽测试数据包：纯负载数据，无有效信息
    BANDWIDTH_TEST_DATA = 8; 
}
```

### 3.2 控制信令 (BandwidthTestRequest)
这是核心控制消息，用于 Server 和 Client 之间同步测试状态。

```protobuf
message BandwidthTestRequest {
    enum Action {
        START = 0; // 开始测试
        STOP = 1;  // 停止测试/结束流
    }
    
    enum Type {
        DOWNLOAD = 0; // Server -> Client (下行)
        UPLOAD = 1;   // Client -> Server (上行)
    }
    
    Action action = 1;     // 动作：开始或停止
    Type type = 2;         // 测试类型：上传或下载
    string session_id = 3; // 会话ID，用于区分并发测试
    int32 duration = 4;    // 持续时间(秒)，仅 START 时有效
}
```

### 3.3 结果汇报 (BandwidthTestResponse)
测试结束后，由接收方（Client或Server）向发起方汇报统计结果。

```protobuf
message BandwidthTestResponse {
    string session_id = 1;
    bool success = 2;
    string err_msg = 3;
    
    int64 bytes_transferred = 4; // 实际传输的应用层字节数
    int64 duration_ms = 5;       // 实际持续时长(毫秒)
    double bandwidth_mbps = 6;   // 计算出的带宽 (Mbps)
}
```

## 4. 详细交互流程

### 4.1 下行带宽测试 (Download Test)
**场景**：Server 发送数据 -> Client 接收并计算

| 步骤 | 发起方 | 消息类型 | 内容/动作 | 说明 |
| :--- | :--- | :--- | :--- | :--- |
| 1 | Server | `BANDWIDTH_TEST_REQ` | `Action: START`<br>`Type: DOWNLOAD`<br>`Duration: 5s` | 通知 Client 准备接收数据，重置计数器。 |
| 2 | Server | `BANDWIDTH_TEST_DATA` | `Payload: 32KB Random Bytes` | Server 循环发送垃圾数据包，持续 5 秒。 |
| ... | ... | ... | ... | ... |
| 3 | Server | `BANDWIDTH_TEST_REQ` | `Action: STOP`<br>`Type: DOWNLOAD` | **关键点**：Server 发送 Stop 信号，告知 Client 数据发送完毕。 |
| 4 | Client | `BANDWIDTH_TEST_RESPONSE` | `Bytes: 104857600`<br>`Duration: 5002ms`<br>`Mbps: 167.7` | Client 收到 Stop 后，停止计时，计算结果并回传给 Server。 |
| 5 | Server | (Internal Log/Event) | - | Server 记录或展示测试结果。 |

**Client 处理逻辑**：
1.  收到 `START`：`bytes_received = 0`, `start_time = Now()`。
2.  收到 `DATA`：`bytes_received += len(payload)`。
3.  收到 `STOP`：
    -   `duration = Now() - start_time`。
    -   `mbps = (bytes_received * 8) / (duration * 1000 * 1000)`。
    -   发送 `Response`。

### 4.2 上行带宽测试 (Upload Test)
**场景**：Client 发送数据 -> Server 接收并计算

| 步骤 | 发起方 | 消息类型 | 内容/动作 | 说明 |
| :--- | :--- | :--- | :--- | :--- |
| 1 | Server | `BANDWIDTH_TEST_REQ` | `Action: START`<br>`Type: UPLOAD`<br>`Duration: 5s` | 通知 Client 开始上传数据。 |
| 2 | Client | `BANDWIDTH_TEST_DATA` | `Payload: 32KB Random Bytes` | Client 启动 Goroutine 循环发送数据包，持续 5 秒。 |
| ... | ... | ... | ... | ... |
| 3 | Client | `BANDWIDTH_TEST_REQ` | `Action: STOP`<br>`Type: UPLOAD` | Client 时间到，主动发送 Stop 信号给 Server。 |
| 4 | Server | `BANDWIDTH_TEST_RESPONSE` | `Mbps: 50.5` | Server 计算结果，**可选**是否回传给 Client（通常 Server 端记录即可）。 |

### 4.2 结束标记发送详解 (Sending Stop Signal)

**关键点**：`STOP` 信号不是一个特殊字符或空包，而是一个完整的 Protocol Buffers 消息。

#### 在 Go 代码中构造发送 Logic (Server 端示例)

```go
// 1. 定义消息结构体 - 设置 Action 为 STOP
req := &pb.BandwidthTestRequest{
    Action:    pb.BandwidthTestRequest_STOP, // <--- 这就是结束标记
    Type:      pb.BandwidthTestRequest_DOWNLOAD,
    SessionId: sessionID,
}

// 2. 序列化 Paylaod
payload, _ := proto.Marshal(req)

// 3. 构造外层消息 (Message)
msg := &pb.Message{
    Type:      pb.MessageType_BANDWIDTH_TEST_REQ, // 注意是 REQ 类型
    SessionId: sessionID,
    Payload:   payload,
}

// 4. 最终序列化并发送
data, _ := proto.Marshal(msg)
tunnel.write(data) // 通过 WebSocket 发送出去
```

当 Client 收到这个消息时：
1. 解析外层 `Message`，发现 Type 是 `BANDWIDTH_TEST_REQ`。
2. 解析 `Payload` 为 `BandwidthTestRequest`。
3. 检查 `Action` 字段，发现是 `STOP`。
4. **立即停止** 接收数据统计，开始计算并汇报结果。

*   预先分配一个全局的 `staticBuffer` (如 32KB)，填充一次随机数据，然后反复发送这同一个 buffer。

### 5.2 结束标志 (Action: STOP)
*   **为什么要显式 Stop？**：如果仅仅依赖时间，Client 和 Server 的时钟可能有微小差异，或者网络本身有延迟。显式的 Stop 信号能确保双方对"数据流何时结束"达成一致，避免 Client 还在傻等数据。
*   **处理乱序**：WebSocket 是有序的，所以 `STOP` 消息一定是在所有 `DATA` 消息之后到达，这简化了逻辑。

### 5.3 错误处理
*   如果 Client 在测试中途断开连接，测试应自动终止，记录失败。
*   如果 Client 收到 `START` 但已经在进行另一个测试，应拒绝并返回 Error。

### 6.6 互斥与冲突处理 (Mutex Policy)
**原则**：同一个 Tunnel（物理设备）在同一时间只能运行一个测试任务。

*   **Server 端检查**：在发起测试前，必须检查目标 Tunnel 是否处于 `Testing` 状态。
*   **状态管理**：Server 需要在一个并发安全的 Map 中维护正在测试的 Tunnel ID。
*   **冲突响应**：如果尝试测试一个正在忙碌的 Tunnel，API 应返回错误：`"tunnel xxx is busy with another test"`。
*   **Client 端防御**：作为双重保险，Client 如果收到 `START` 指令但自身正在测试，应回复 Error。


为了方便管理员操作，我们将在 `ippop` 提供一个 HTTP API 触发测试。

### 6.1 单个/指定测试
```http
POST /api/tunnel/bandwidth-test
{
    "tunnel_ids": ["tunnel-123", "tunnel-456"], // 指定测试这几个 ID
    "type": "download",  // or "upload"
    "duration": 5
}
```

### 6.2 随机批量测试 (New Feature)
允许并在一定范围内随机抽取在线设备进行群测。

```http
POST /api/tunnel/bandwidth-test/random
{
    "count": 10,         // 随机选 10 个在线设备
    "concurrency": 2,    // 并发度：同时测试几个设备 (默认 1)
    "type": "upload", 
    "duration": 10
}
```

### 6.3 响应结构
由于批量测试耗时较长，API 应该返回一个 `batch_id`，管理员后续通过该 ID 查询结果。

```json
{
    "success": true,
    "batch_id": "batch-20231027-001",
    "message": "Started testing 10 devices in background"
}
```

### 6.4 查询测试结果 (Query Results)

管理员需要查询刚刚发起的测试结果，特别是对于耗时的批量测试。

```http
GET /api/tunnel/bandwidth-test/batch/:batch_id
```

**响应示例**：

```json
{
    "batch_id": "batch-20231027-001",
    "status": "in_progress", // waiting, in_progress, completed
    "progress": "5/10",      // 已完成 5 个，共 10 个
    "results": [
        {
            "tunnel_id": "tunnel-123",
            "status": "completed",
            "type": "download",
            "bandwidth_mbps": 150.5,
            "duration_ms": 5002,
            "error": ""
        },
        {
            "tunnel_id": "tunnel-456",
            "status": "pending", // 还在队列中排队
            "bandwidth_mbps": 0,
            "error": ""
        }
    ]
}
```

### 6.5 并发控制 (Concurrency Control)
*   **策略**：由 API 请求参数 `concurrency` 控制并行度。
*   **执行逻辑**：Server 将任务放入队列，每次同时执行 `N = min(concurrency, MAX_SAFE_LIMIT)` 个测试。
*   **安全上限**：Server 端应设置一个硬性上限（例如 50），防止调用者传入过大数值导致 Server 崩溃。


---
**确认**：通过使用 `Action: STOP` 消息，我们可以精准控制下行测试的结束，无需依赖复杂的超时猜测或特殊 payload 标记。
