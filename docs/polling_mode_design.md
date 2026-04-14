# IPPop 轮询模式 (Polling Mode) 设计方案文档

## 1. 背景与目标
在当前的 IP 资源管理逻辑中，IP 分配采用的是**独占模式 (Exclusive Mode)**：
- 调用 `AcquireIP` 时，IP 会从空闲列表 (`freeList`) 中移除。
- IP 会记录 `assignedNodeID` 以标记占用状态。
- 只有调用 `ReleaseIP` 后，IP 才会返回空闲池。

**目标**：引入一种**轮询模式 (Polling Mode)**，允许用户在不减少 IP 池可用数量的前提下，以负载均衡的方式共享使用所有可用出口 IP，适用于高性能、高并发且对 IP 独占性要求不高的场景。

---

## 2. 核心设计原则
为了满足“不占用、不释放、共享IP”的需求，轮询模式遵循以下原则：

### 2.1 不占用 IP (Non-occupying)
- **行为**：在分配 IP 时，直接从 `IPPool` 的 `freeList` 中读取 IP 信息，但**不执行** `removeFromFreePool` 操作。
- **影响**：IP 池的统计数据（如 `FreeIPCount`）不会下降。该 IP 对其他模式的用户依然是“可见”且“可用”的。

### 2.2 不释放 IP (Non-releasing)
- **行为**：Session 生命周期结束时，由于分配阶段没有标记 `assignedNodeID`，系统无需调用 `ReleaseIP`。
- **影响**：避免了频繁操作 IP 池锁（Mutex）和维护分配状态的开销，提升了高并发下的系统吞吐量。

### 2.3 共享 IP (Shared)
- **行为**：轮询模式用户与独占模式用户共享相同的 IP 资源池。
- **冲突处理**：即使一个 IP 正在被独占用户使用，轮询用户依然可以通过负载均衡策略随机到该 IP 对应的隧道上。

---

## 3. 技术实现方案 (逻辑架构)

### 3.1 路由模式定义
在 `model/user.go` 中新增路由模式枚举：
- `RouteModePolling (5)`：代表轮询共享模式。

### 3.2 轮询分配器 (PollingAllocator)
实现一个新的分配策略类 `PollingAllocator`（继承 `NodeAllocator` 接口）：
1. **获取快照**：调用 `IPPool` 提供的新接口 `GetFreeIPsSnapshot()`，获取当前所有空闲 IP 的列表。
2. **原子轮询**：分配器内部维护一个全局原子计数器 `rrCounter`。
3. **索引计算**：`index = atomic.AddUint64(&rrCounter, 1) % len(snapshot)`。
4. **隧道选择**：选定 IP 后，从该 IP 对应的 `ipEntry` 中随机选择一个存活的 `Tunnel`。
5. **Session 属性**：返回的 `UserSession` 标记为 `isEphemeral = true`（临时会话），且 `exitIP` 保持为空，确保其生命周期结束时不会触发复杂的释放逻辑。

### 3.3 IP 池层支持 (`IPPool`)
为了配合轮询模式，`IPPool` 需要提供一种非破坏性的读取方式：
- **`PeekLRUIP()`**：只读取 `freeList` 头部或尾部的 IP 指针，而不进行 `Remove`。
- **`BalanceAcrossLines()`**：利用现有的 `localIPFreeList`，在多条带宽线路之间进行轮询，确保出口流量在不同物理线路上分布均匀。

---

## 4. 优势
1. **资源利用率最大化**：出口 IP 不再受限于“一对一”的绑定关系，一个 IP 可以同时承载成百上千个并发连接。
2. **系统低损耗**：由于跳过了 IP 状态转换（Free -> Busy -> Free）的过程，减少了对 Redis 的写操作。
3. **无缝兼容**：原有独占模式用户依然能保证拿到干净的 IP，而轮询用户则在剩余的 IP 空间中进行高效流动。

---

## 5. 结论
轮询模式通过**零侵入性**的分配方式，在不改变 IP 池核心数据结构的前提下，实现了资源的共享。它将 IP 从“被管辖的资产”转变为“可引流的端点”，极大地增强了 `ippop` 在高负载环境下的扩展能力。
