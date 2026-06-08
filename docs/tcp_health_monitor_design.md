# Titan-DNS TCP 端口健康监控与 IP 动态黑名单设计方案

本设计方案旨在为 `titan-dns` 增加周期性的 TCP 端口健康检测与动态黑名单过滤机制。
其核心功能为：**定时对所有配置的 POP IP 进行 TCP 端口连接测试，若测试失败则拉黑该 IP，在 DNS 解析轮询时不返回该 IP；当 IP 恢复通畅时自动移出黑名单。**

---

## 核心设计思想：动态遍历过滤（Dynamic Traversal Filter）

为了保证 DNS 解析的高并发性能、消除垃圾回收（GC）开销，同时避免复杂的写时同步，我们采用**动态遍历过滤**：
1. **静态配置不变**：`LoadBalancer` 的 IP 列表只从配置文件或 API 写入，运行期间不做增删，保证数据一致性与安全性（避免动态更新污染配置文件）。
2. **并发安全黑名单**：使用 `sync.Map` 维护一个全局黑名单，提供 $O(1)$ 的并发读写，无锁竞争风险。
3. **原地无内存分配轮询**：在轮询 IP 时，利用现有的 Round-Robin 计数器作为偏移起点，通过取模运算在原数组上顺时针遍历，跳过黑名单 IP，零内存分配。

---

## 详细技术细节设计

### 1. 配置管理设计
在配置文件 `config.yaml` 中新增 `monitor` 配置块。为了方便管理，参数以基础类型定义，避免解析复杂：

* **YAML 配置 (config.yaml)**
  ```yaml
  monitor:
    enabled: true
    port: 8080           # 可配置的 TCP 检测端口
    interval_seconds: 30 # 定时检测间隔（秒）
    timeout_seconds: 3   # TCP Dial 超时时间（秒）
    unhealthy_threshold: 3 # 连续失败多少次后加入黑名单
    concurrency_limit: 5   # 最大并发探测数
  ```

* **配置解析结构体 (config.go)**
  ```go
  type Config struct {
      Server  ServerConfig  `yaml:"server"`
      Monitor MonitorConfig `yaml:"monitor"` // 新增监控配置字段
      Pops    []PopConfig   `yaml:"pops"`
  }

  type MonitorConfig struct {
      Enabled            bool `yaml:"enabled"`
      Port               int  `yaml:"port"`
      IntervalSeconds    int  `yaml:"interval_seconds"`
      TimeoutSeconds     int  `yaml:"timeout_seconds"`
      UnhealthyThreshold int  `yaml:"unhealthy_threshold"`
      ConcurrencyLimit   int  `yaml:"concurrency_limit"`
  }
  ```

---

### 2. 黑名单与后台健康检测设计
新建 `monitor.go` 管理黑名单与并发 TCP 探测。我们将整个监控逻辑封装在独立的 `TCPMonitor` 结构中，防止污染 `DNSHandler`。

* **IP 状态计数器**
  ```go
  type IPStatusTracker struct {
      mu                  sync.Mutex
      consecutiveFailures int // 仅跟踪连续失败次数
  }
  ```

* **并发安全黑名单 (Blacklist)**
  使用 Go 的 `sync.Map` 实现。DNS 查询是读多写极少的场景，`sync.Map` 在此场景下读性能接近无锁。
  ```go
  type Blacklist struct {
      ips sync.Map // map[string]bool
  }

  func (b *Blacklist) Add(ip string)    { b.ips.Store(ip, true) }
  func (b *Blacklist) Remove(ip string) { b.ips.Delete(ip) }
  func (b *Blacklist) Contains(ip string) bool {
      _, ok := b.ips.Load(ip)
      return ok
  }
  ```

* **监控对象设计 (TCPMonitor)**
  监控模块封装为 `TCPMonitor`，通过回调函数动态安全地获取配置和负载均衡器实例：
  ```go
  type TCPMonitor struct {
      getConfig   func() MonitorConfig
      getBalancer func() *LoadBalancer
      blacklist   *Blacklist
      history     sync.Map // 存储 ip -> *IPStatusTracker
  }

  func NewTCPMonitor(getConfig func() MonitorConfig, getBalancer func() *LoadBalancer, blacklist *Blacklist) *TCPMonitor {
      return &TCPMonitor{
          getConfig:   getConfig,
          getBalancer: getBalancer,
          blacklist:   blacklist,
      }
  }
  ```

* **探测机制与启动**
  1. 调用 `Start()` 启动健康检查协程。
  2. 定时调用 `checkAllIPs()` 取得当前所有 POP 的唯一 IP 列表。
  3. 针对每一个 IP，启动一个独立的 Goroutine 进行并发 TCP 探测。
  4. 当 IP 探测失败：
     - 累加失败计数 `Failures++`。
     - 当连续失败次数达到 `unhealthy_threshold` 时，将该 IP 加入 `blacklist`。
  5. 当 IP 探测成功：
     - 重置连续失败计数 `Failures = 0`。
     - **只要有一次成功，立刻**将该 IP 从 `blacklist` 移出并恢复解析。

---

### 3. DNS 轮询与黑名单过滤逻辑设计
这是本次设计的核心。在 `balancer.go` 的 `BalanceByRR` 方法中：

```go
func (lb *LoadBalancer) GetAllUniqueIPs() []string {
	lb.mu.RLock()
	defer lb.mu.RUnlock()
	ipMap := make(map[string]bool)
	var ips []string
	for _, pop := range lb.pops {
		if pop.Ref != "" {
			continue
		}
		for _, ip := range pop.IPs {
			if !ipMap[ip] {
				ipMap[ip] = true
				ips = append(ips, ip)
			}
		}
	}
	return ips
}

func (lb *LoadBalancer) BalanceByRR(popID string, isBlacklisted func(string) bool) (string, uint64) {
	lb.mu.RLock()
	data, ok := lb.pops[popID]
	ipsData := data
	if ok && data.Ref != "" {
		if nextData, nextOk := lb.pops[data.Ref]; nextOk {
			ipsData = nextData
		}
	}
	lb.mu.RUnlock()

	if !ok || ipsData == nil || len(ipsData.IPs) == 0 {
		return "", 0
	}

	n := uint64(len(ipsData.IPs))

	// 顺时针顺序遍历所有 IP 检查其可用性，每次尝试均递增 rrIndex 推进槽位
	for i := uint64(0); i < n; i++ {
		index := atomic.AddUint64(&data.rrIndex, 1) - 1
		currIndex := index % n
		ip := ipsData.IPs[currIndex]
		if !isBlacklisted(ip) {
			return ip, index // 找到首个不在黑名单的可用 IP，直接返回
		}
	}

	// 3. 若所有 IP 都被拉黑，根据约定返回空，避免返回故障 IP
	return "", 0
}
```

* **为何是零开销**：此算法遍历的上限为 `N`（POP 内 IP 数量），并且仅在原切片上通过下标偏移遍历，不进行任何切片复制、扩容或追加操作，内存分配为 0。同时把 `atomic.AddUint64` 放入循环中，可以在遇到黑名单时自动消耗和跳过对应的槽位。

---

### 4. 会话缓存（Sticky Cache）覆盖机制设计
`titan-dns` 拥有一个会话缓存 `StickyCache`，用于保持同一个 session 总是路由到相同的 IP。
如果该 IP 变为了黑名单 IP，我们需要确保后续请求不再返回缓存中的坏 IP：

* 在 `handler.go` 的 `resolveSubdomain` 中：
  ```go
  if ip, ok := h.cache.Get(name); ok {
      // 检查缓存的 IP 是否被拉黑
      if !h.blacklist.Contains(ip) {
          return ip, 0 // 依然健康，直接返回缓存 IP
      }
      // 已被拉黑，旁路缓存，重新走负载均衡轮询获取健康 IP
  }

  // 走 Balance 选出一个健康的 IP
  ip, index := h.balancer.BalanceBySession(popID, sessionID, h.blacklist.Contains)
  if ip != "" {
      h.cache.Set(name, ip) // 新的健康 IP 覆盖写入缓存，自动驱逐故障 IP
  }
  return ip, index
  ```

---

### 5. 别名（Ref）与跟随（Follow）关系自动继承
* **Ref 别名**：`BalanceByRR` 会首先解析 `Ref` 指向的基准 POP 数据（`ipsData = nextData`）。因此当基准 POP 的 IP 被拉黑时，引用别名的子 POP 在遍历时会自动感知该黑名单状态，无需任何同步逻辑。
* **Follow 关系**：跟随者关系会通过组合父 POP 的 IP 列表存入 `data.IPs` 中。监控探测会获取这批合并后的 IP。当其中某个父 POP 的 IP 挂掉被拉黑时，跟随者 POP 在遍历自己的 `data.IPs` 时，遍历逻辑依然会在 `isBlacklisted` 校验中将其过滤，逻辑完全自适应。

---

## 验证计划

### 自动化测试
编写 `test/monitor_test.go`：
1. 启动本地 Mock TCP 监听服务，模拟正常的 IP 节点。
2. 配置 `titan-dns` 节点，包含存活 IP 和未存活的 IP。
3. 调用 `BalanceByRR` 确认仅返回存活的 IP。
4. 关闭 Mock 监听服务，验证经过间隔时间后该 IP 自动进入黑名单，解析返回 `""`。
5. 重新启动 Mock 服务，验证 IP 自动移出黑名单并正常恢复解析。

### 手动验证
1. 启动 `titan-dns` 并配置两个测试 IP（如本地监听的 `127.0.0.1` 和未监听 of `127.0.0.2`，端口设为 `9000`）。
2. 在 `9000` 端口启动测试服务，测试 DNS 查询，确认轮询两个 IP。
3. 关掉测试服务，查看日志，应该输出：`[MONITOR] IP 127.0.0.1 is UNREACHABLE...`。
4. 再次执行 `nslookup` 或 `dig`，确认不在黑名单的 IP 得以返回，而挂掉的 IP 不再返回。
