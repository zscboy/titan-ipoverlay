package ws

import (
	"sync"
	"sync/atomic"
)

// ephemeralIPCounter 统计每用户当前持有的「临时独占出口 IP」数量。
//
// 用户名中不带 -session- 参数时，SessionAllocator 仍会为这条连接独占一个出口 IP
// （allocator_session.go 中 target.Session == "" 的分支），但该会话不会写入
// SessionManager.sessions / userIndex，连接结束即释放。若不单独统计，
// ippop_user_exclusive_ips 会漏掉这部分占用，运维会低估用户实际吃掉的 IP 资源。
//
// 这里刻意不复用 SessionManager.lock：临时会话的释放路径（Decrement 中
// isEphemeral 分支）是无锁快路径，引入全局写锁会直接放大热路径延迟。
// sync.Map + atomic 让 inc/dec 保持 O(1) 且互不阻塞。
type ephemeralIPCounter struct {
	counts sync.Map // username -> *atomic.Int64
}

func (e *ephemeralIPCounter) counterFor(username string) *atomic.Int64 {
	if v, ok := e.counts.Load(username); ok {
		return v.(*atomic.Int64)
	}
	v, _ := e.counts.LoadOrStore(username, new(atomic.Int64))
	return v.(*atomic.Int64)
}

func (e *ephemeralIPCounter) inc(username string) {
	e.counterFor(username).Add(1)
}

func (e *ephemeralIPCounter) dec(username string) {
	e.counterFor(username).Add(-1)
}

// snapshot 返回当前非零的用户占用量，供 Prometheus Collector 在抓取时使用。
// 计数为 0 的用户不导出序列，因此不会在看板上留下常驻的 0 值噪声。
//
// 这里刻意 **不** 在抓取时回收归零条目。曾经的实现用
// CompareAndDelete(key, value) 回收，但那是错的：CompareAndDelete 比的是
// **指针**，不是计数值。抓取协程 Load 出 0 之后、CompareAndDelete 执行之前，
// 若有 inc() 把同一个计数器加到 1，比较依然成立，于是把一个正在使用的计数器
// 从 map 里删掉；配对的 dec() 随后 LoadOrStore 出一个全新计数器并减成 -1。
// 而 -1 既不会被导出（只导出 n>0）也不会被回收（只回收 n==0），
// 该账号从此永久少算一个 IP，且每命中一次就再少一个。
// 已由 TestEphemeralIPCounterNoDriftUnderConcurrentSnapshot 复现。
//
// 不回收是安全的：key 是账号名，而账号必须先通过 Redis 鉴权才能建连，
// 其取值范围就是我们已经接受的 user 标签基数（数十~数百），不会无界增长。
func (e *ephemeralIPCounter) snapshot() map[string]int64 {
	out := make(map[string]int64)
	e.counts.Range(func(key, value any) bool {
		if n := value.(*atomic.Int64).Load(); n > 0 {
			out[key.(string)] = n
		}
		return true
	})
	return out
}
