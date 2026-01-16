# 模块设计文档：通用节点分配器 (Generic Node Allocator)

## 1. 设计目标
*   **极致性能**：节点检索与会话校验复杂度为 $O(1)$，无临时内存分配。
*   **资源隔离**：确保每个 Session ID 独占一个物理节点（Exclusive Lease），不同 Session 绝不混用。
*   **零侵入解耦**：分配逻辑通过接口与 Redis 及底层 IO 隔离，支持 Mock 测试。
*   **高效回收**：采用“延迟回收队列”机制，消除高并发下的全量扫描开销。

---

## 2. 核心架构设计

### 2.1 依赖关系 (Dependency Inversion)
*   **NodeAllocator (策略层)**：只负责业务逻辑判断。
*   **NodeSource (驱动层)**：由 `TunnelManager` 实现，负责真实的节点申请与锁定。
*   **SessionManager (存储层)**：维护内存中的 Session 状态，并管理生命周期。

### 2.2 核心接口定义

#### NodeSource (资源抽象)
```go
type NodeSource interface {
    AcquireExclusiveNode(ctx context.Context) (*Tunnel, error)
    ReleaseExclusiveNode(nodeID string)
    GetLocalTunnel(nodeID string) *Tunnel
}
```

---

## 3. 关键实现细节

### 3.1 O(1) 检索实现
使用复合结构体键 `sessionKey` 实现单表 $O(1)$ 寻址，避免字符串拼接。
```go
type sessionKey struct {
    username  string
    sessionID string
}
```

### 3.2 延迟回收队列 (Idle Cleanup Queue)
*   **入队**：当 `connectCount` 归零时，Session 对象的引用被插入双向链表 `idleList` 末尾。
*   **清理**：`Cleaner` 协程只扫描 `idleList` 头部。若头部元素未超时，则整队跳过。
*   **二次确认**：清理前校验 `connectCount` 是否仍为 0，防止复用期间被错误回收。

### 3.3 独占租赁 (Exclusive Lease)
*   通过 `NodeSource.AcquireExclusiveNode` 确保返回的节点 `isLeased = true`。
*   在全局范围内（Redis 实现层）保证节点不会被分配给第二个请求者。

---

## 4. 内存估算
单个 Session 包含 Struct Key、指针、Session 对象及基础字符串数据，约占用 **250 字节**。
百万级 Session 仅需约 **250 MB** 内存。
