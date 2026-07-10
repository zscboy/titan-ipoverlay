package ws

import (
	"encoding/binary"
	"fmt"
	"sync"
	"testing"
	"time"
)

func mkTun(ip, id string, delayMs int64) *Tunnel {
	t := &Tunnel{opts: &TunOptions{IP: ip, Id: id}}
	t.delay.Store(delayMs)
	t.lastPongAt.Store(time.Now().UnixMilli()) // Task 3: 启用
	return t
}

func fillPool(n int, delayOf func(i int) int64) *IPPool {
	p := NewIPPool()
	for i := 0; i < n; i++ {
		p.AddTunnel(mkTun(fmt.Sprintf("10.0.0.%d", i), fmt.Sprintf("n%d", i), delayOf(i)), false)
	}
	return p
}

// assertSliceInvariant: pollSlice 与 freeList 成员一一对应且下标互指。
func assertSliceInvariant(t *testing.T, p *IPPool) {
	t.Helper()
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.pollSlice) != p.freeList.Len() {
		t.Fatalf("pollSlice len %d != freeList len %d", len(p.pollSlice), p.freeList.Len())
	}
	for i, e := range p.pollSlice {
		if e.sliceIdx != i {
			t.Fatalf("entry %s sliceIdx=%d but at index %d", e.ip, e.sliceIdx, i)
		}
		if e.element == nil {
			t.Fatalf("entry %s in pollSlice but not in freeList", e.ip)
		}
	}
	// 反向遍历：freeList → pollSlice 方向的完整双射验证。
	for el := p.freeList.Front(); el != nil; el = el.Next() {
		e := el.Value.(*ipEntry)
		if e.sliceIdx < 0 {
			t.Fatalf("entry %s in freeList but sliceIdx=%d", e.ip, e.sliceIdx)
		}
		if p.pollSlice[e.sliceIdx] != e {
			t.Fatalf("entry %s in freeList but pollSlice[%d] does not point back to it", e.ip, e.sliceIdx)
		}
	}
}

// 交错 加盒/摘盒/独占取还/拉黑 后不变式必须恒成立。
func TestP2C_PollSliceInvariant(t *testing.T) {
	p := fillPool(10, func(i int) int64 { return 100 })
	assertSliceInvariant(t, p)

	// 独占取走 3 个（removeFromFreePool 路径）
	var ips []string
	for i := 0; i < 3; i++ {
		ip, tun := p.AcquireIP("")
		if tun == nil {
			t.Fatal("AcquireIP returned nil")
		}
		ips = append(ips, ip)
		assertSliceInvariant(t, p)
	}
	// 归还（addToFreePool 路径）
	for _, ip := range ips {
		p.ReleaseIP(ip)
		assertSliceInvariant(t, p)
	}
	// 拉黑摘除
	p.DeactivateIP("10.0.0.5")
	assertSliceInvariant(t, p)
	// 新盒上线
	p.AddTunnel(mkTun("10.0.0.99", "n99", 50), false)
	assertSliceInvariant(t, p)
	// 盒子下线
	tuns := p.GetTunnelsByIP("10.0.0.7")
	for _, tun := range tuns {
		p.RemoveTunnel(tun)
	}
	assertSliceInvariant(t, p)
}

func TestTunnel_OnPongUpdatesLastPongAt(t *testing.T) {
	tun := &Tunnel{opts: &TunOptions{IP: "10.0.0.1", Id: "n1"}}
	if tun.lastPongAt.Load() != 0 {
		t.Fatal("lastPongAt should start at 0")
	}
	data := make([]byte, 8)
	binary.LittleEndian.PutUint64(data, uint64(time.Now().Add(-10*time.Millisecond).UnixMicro()))
	tun.onPong(data)
	if tun.lastPongAt.Load() == 0 {
		t.Fatal("onPong must stamp lastPongAt")
	}
	if tun.delay.Load() < 0 {
		t.Fatalf("delay should be >= 0, got %d", tun.delay.Load())
	}
}

// P2C 测试通用参数：RREvery 拉满 = 关掉保底，只测竞速路径。
func raceOnly(depth int) P2CParams {
	return P2CParams{Depth: depth, RREvery: 1 << 30, LambdaMs: 100, MinPool: 2}
}

// 竞速显著压低被选 RTT：一半快盒(100ms)一半慢盒(900ms)，D=2 理论选快概率 1-(1/2)^2=75%，均值≈300。
func TestP2C_RaceLowersSelectedRTT(t *testing.T) {
	p := fillPool(40, func(i int) int64 {
		if i < 20 {
			return 100
		}
		return 900
	})
	var sum int64
	for c := 0; c < 400; c++ {
		_, tun := p.AcquireP2CPollingIP(raceOnly(2))
		if tun == nil {
			t.Fatal("nil tunnel")
		}
		sum += tun.delay.Load()
	}
	mean := float64(sum) / 400
	if mean >= 450 { // 均匀轮询≈500，竞速必须显著更低（理论≈300，留宽容余量）
		t.Fatalf("selected mean RTT %.0f, want << 500", mean)
	}
}

// λ 惩罚：快盒全部高在途(20 条会话)，λ=100 时 score=100+2000 输给闲慢盒(800)。
func TestP2C_LambdaPenalizesLoadedBox(t *testing.T) {
	p := fillPool(16, func(i int) int64 {
		if i < 4 {
			return 100 // 快但忙
		}
		return 800 // 慢但闲
	})
	p.mu.Lock()
	for i := 0; i < 4; i++ {
		e := p.allIPs[fmt.Sprintf("10.0.0.%d", i)]
		for _, tun := range e.tunnels {
			for s := 0; s < 20; s++ {
				tun.proxys.Store(fmt.Sprintf("sess-%d", s), s)
			}
		}
	}
	p.mu.Unlock()

	fast := 0
	for c := 0; c < 300; c++ {
		ip, tun := p.AcquireP2CPollingIP(raceOnly(2))
		if tun == nil {
			t.Fatal("nil tunnel")
		}
		var idx int
		fmt.Sscanf(ip, "10.0.0.%d", &idx)
		if idx < 4 {
			fast++
		}
	}
	// 无 λ 时快盒会赢下所有含它的竞速(≈43%)；有 λ 时只剩双样本都是快盒的情形(≈6%)+噪声。
	if fast > 45 {
		t.Fatalf("loaded fast boxes won %d/300 races, lambda penalty not working", fast)
	}
}

// 冷启动：全池 delay=0 → 竞速全弃权 → 退化为轮询队首，前 n 次选中 n 个不同 IP。
func TestP2C_ColdStartFallsBackToPolling(t *testing.T) {
	p := fillPool(10, func(i int) int64 { return 0 })
	seen := map[string]bool{}
	for c := 0; c < 10; c++ {
		ip, tun := p.AcquireP2CPollingIP(raceOnly(2))
		if tun == nil {
			t.Fatal("nil tunnel")
		}
		seen[ip] = true
	}
	if len(seen) != 10 {
		t.Fatalf("cold start should rotate all 10 IPs, got %d distinct", len(seen))
	}
}

// 陈旧 delay 弃权：唯一的"超快"盒 lastPongAt 在 120s 前 → 不得靠竞速胜出。
func TestP2C_StaleDelayAbstains(t *testing.T) {
	p := fillPool(16, func(i int) int64 { return 500 })
	p.mu.Lock()
	e := p.allIPs["10.0.0.0"]
	for _, tun := range e.tunnels {
		tun.delay.Store(1) // 假装最快
		tun.lastPongAt.Store(time.Now().Add(-120 * time.Second).UnixMilli())
	}
	p.mu.Unlock()

	wins := 0
	for c := 0; c < 300; c++ {
		ip, _ := p.AcquireP2CPollingIP(raceOnly(2))
		if ip == "10.0.0.0" {
			wins++
		}
	}
	// 若陈旧检查缺失，它会赢下所有含它的竞速(≈12%×300=36+)；正常只能偶发走全弃权 fallback。
	if wins > 15 {
		t.Fatalf("stale box selected %d/300 times, staleness check not working", wins)
	}
}

// MaxBoxSessions 硬上限：在途 ≥ 上限的盒子弃权。
// 其他盒 delay=900（而非 500）：确保 box0 分数 1+100×5=501 < 900，无 cap 时 box0
// 会赢下所有含它的竞速，唯有 cap 检查生效才让它弃权 → 测试真正判别 cap 逻辑。
func TestP2C_MaxBoxSessionsAbstains(t *testing.T) {
	p := fillPool(16, func(i int) int64 { return 900 })
	p.mu.Lock()
	e := p.allIPs["10.0.0.0"]
	for _, tun := range e.tunnels {
		tun.delay.Store(1)
		for s := 0; s < 5; s++ {
			tun.proxys.Store(fmt.Sprintf("sess-%d", s), s)
		}
	}
	p.mu.Unlock()

	cfg := raceOnly(2)
	cfg.MaxBoxSessions = 3
	wins := 0
	for c := 0; c < 300; c++ {
		ip, _ := p.AcquireP2CPollingIP(cfg)
		if ip == "10.0.0.0" {
			wins++
		}
	}
	if wins > 15 {
		t.Fatalf("over-capacity box selected %d/300 times", wins)
	}
}

// R=1 滑轨：逐请求等价原混播（与 AcquirePollingIP 产生完全相同的轮转序列）。
func TestP2C_RREveryOneEqualsPlainPolling(t *testing.T) {
	pA := fillPool(10, func(i int) int64 { return int64(100 + i) })
	pB := fillPool(10, func(i int) int64 { return int64(100 + i) })
	cfg := P2CParams{Depth: 2, RREvery: 1, LambdaMs: 100, MinPool: 2}
	for c := 0; c < 30; c++ {
		ipA, _ := pA.AcquireP2CPollingIP(cfg)
		ipB, _ := pB.AcquirePollingIP()
		if ipA != ipB {
			t.Fatalf("call %d: P2C(R=1) got %s, plain polling got %s", c, ipA, ipB)
		}
	}
}

// 薄池护栏：n < MinPool 时整体退化为轮询。
func TestP2C_ThinPoolFallsBack(t *testing.T) {
	p := fillPool(8, func(i int) int64 { return int64(10 * (i + 1)) })
	cfg := P2CParams{Depth: 2, RREvery: 1 << 30, LambdaMs: 100, MinPool: 16}
	seen := map[string]bool{}
	for c := 0; c < 8; c++ {
		ip, _ := p.AcquireP2CPollingIP(cfg)
		seen[ip] = true
	}
	if len(seen) != 8 {
		t.Fatalf("thin pool should rotate all 8 IPs, got %d distinct", len(seen))
	}
}

func TestP2C_EmptyPool(t *testing.T) {
	p := NewIPPool()
	if ip, tun := p.AcquireP2CPollingIP(raceOnly(2)); tun != nil || ip != "" {
		t.Fatal("empty pool must return nil")
	}
}

// 日光门槛①：n×R 次分配内全池每个 IP 至少被选中一次（有界饥饿的构造性证明）。
// 保底路径每次取 LRU 队首，而从未被选中的盒子不会被 rotate，必然沉在队首区，
// 因此每个保底名额都喂给未被选中者（若还有）——n×R 次内必然全覆盖。
func TestP2C_FullCoverageWithinNxR(t *testing.T) {
	const n, r = 40, 5
	p := fillPool(n, func(i int) int64 {
		if i < 8 {
			return 50 // 少数快盒吸走大部分竞速流量
		}
		return 800
	})
	cfg := P2CParams{Depth: 2, RREvery: r, LambdaMs: 100, MinPool: 2}
	selected := map[string]int{}
	for c := 0; c < n*r; c++ {
		ip, tun := p.AcquireP2CPollingIP(cfg)
		if tun == nil {
			t.Fatal("nil tunnel")
		}
		selected[ip]++
	}
	for i := 0; i < n; i++ {
		ip := fmt.Sprintf("10.0.0.%d", i)
		if selected[ip] == 0 {
			t.Fatalf("box %s starved after %d allocations (bounded-starvation broken)", ip, n*r)
		}
	}
}

// 日光门槛②：最快盒份额 ≤ (D(1-1/R)+1/R)/n 的 1.5 倍（D=2,R=5 → 1.8/n）。
func TestP2C_FastestShareBounded(t *testing.T) {
	const n = 100
	p := fillPool(n, func(i int) int64 { return int64(100 + i*10) }) // 10.0.0.0 恒最快
	cfg := P2CParams{Depth: 2, RREvery: 5, LambdaMs: 1, MinPool: 2}  // λ=1 保持计分路径、近似最坏情形
	const draws = 50000
	fastest := 0
	for c := 0; c < draws; c++ {
		if ip, _ := p.AcquireP2CPollingIP(cfg); ip == "10.0.0.0" {
			fastest++
		}
	}
	bound := (2.0*(1-1.0/5) + 1.0/5) / n * 1.5 // 理论 1.8% × 1.5 容差
	share := float64(fastest) / draws
	if share > bound {
		t.Fatalf("fastest box share %.4f exceeds bound %.4f", share, bound)
	}
}

// 并发安全冒烟：8 个 goroutine 竞速 + 2 个 goroutine 上下线，-race 下跑完不变式仍成立。
func TestP2C_ConcurrentChurn(t *testing.T) {
	p := fillPool(32, func(i int) int64 { return int64(100 + i) })
	cfg := P2CParams{Depth: 2, RREvery: 5, LambdaMs: 100, MinPool: 2}
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for c := 0; c < 2000; c++ {
				p.AcquireP2CPollingIP(cfg)
			}
		}()
	}
	for g := 0; g < 2; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for c := 0; c < 200; c++ {
				ip := fmt.Sprintf("10.9.%d.%d", g, c)
				tun := mkTun(ip, ip, 100)
				p.AddTunnel(tun, false)
				p.RemoveTunnel(tun)
			}
		}(g)
	}
	wg.Wait()
	assertSliceInvariant(t, p)
}

func TestSanitizeP2CParams(t *testing.T) {
	cases := []struct {
		name        string
		in          P2CParams
		wantDepth   int
		wantRR      int
		wantMinPool int
		wantWarn    bool
	}{
		{"valid V3 passes", P2CParams{Depth: 2, RREvery: 5, LambdaMs: 100, MinPool: 16}, 2, 5, 16, false},
		{"lambda below 50 disables", P2CParams{Depth: 2, RREvery: 5, LambdaMs: 0, MinPool: 16}, 0, 5, 16, true},
		{"lambda 49 disables", P2CParams{Depth: 2, RREvery: 5, LambdaMs: 49, MinPool: 16}, 0, 5, 16, true},
		// 安全门正边界：λ 恰为 50 不得触发禁用，Depth 必须保持 2。
		{"lambda 50 is safe edge", P2CParams{Depth: 2, RREvery: 5, LambdaMs: 50, MinPool: 16}, 2, 5, 16, false},
		{"depth clamped to 4", P2CParams{Depth: 9, RREvery: 5, LambdaMs: 100, MinPool: 16}, 4, 5, 16, true},
		{"depth 5 clamped to 4", P2CParams{Depth: 5, RREvery: 5, LambdaMs: 100, MinPool: 16}, 4, 5, 16, true},
		{"depth 4 kept", P2CParams{Depth: 4, RREvery: 5, LambdaMs: 100, MinPool: 16}, 4, 5, 16, false},
		{"rr clamped to 10", P2CParams{Depth: 2, RREvery: 50, LambdaMs: 100, MinPool: 16}, 2, 10, 16, true},
		{"rr 11 clamped to 10", P2CParams{Depth: 2, RREvery: 11, LambdaMs: 100, MinPool: 16}, 2, 10, 16, true},
		{"rr 10 kept", P2CParams{Depth: 2, RREvery: 10, LambdaMs: 100, MinPool: 16}, 2, 10, 16, false},
		{"rr floor 1 kept as slide rail", P2CParams{Depth: 2, RREvery: 1, LambdaMs: 100, MinPool: 16}, 2, 1, 16, false},
		{"rr 0 set to 5", P2CParams{Depth: 2, RREvery: 0, LambdaMs: 100, MinPool: 16}, 2, 5, 16, false},
		{"minpool floor", P2CParams{Depth: 2, RREvery: 5, LambdaMs: 100, MinPool: 0}, 2, 5, 16, true},
		{"minpool 2 kept", P2CParams{Depth: 2, RREvery: 5, LambdaMs: 100, MinPool: 2}, 2, 5, 2, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out, warn := sanitizeP2CParams(c.in)
			if out.Depth != c.wantDepth {
				t.Fatalf("Depth: got %d, want %d", out.Depth, c.wantDepth)
			}
			if out.RREvery != c.wantRR {
				t.Fatalf("RREvery: got %d, want %d", out.RREvery, c.wantRR)
			}
			if out.MinPool != c.wantMinPool {
				t.Fatalf("MinPool: got %d, want %d", out.MinPool, c.wantMinPool)
			}
			if c.wantWarn != (warn != "") {
				t.Fatalf("warn mismatch: wantWarn=%v got %q", c.wantWarn, warn)
			}
		})
	}
}

// 三条路径的计数器都在动，且 pollSlice 长度与 freeList 一致地报出来。
func TestP2C_StatsCounters(t *testing.T) {
	p := fillPool(16, func(i int) int64 { return 100 })
	cfg := P2CParams{Depth: 2, RREvery: 2, LambdaMs: 100, MinPool: 2}
	for c := 0; c < 100; c++ {
		p.AcquireP2CPollingIP(cfg)
	}
	s := p.GetPoolStats()
	if s.P2CRRHits == 0 || s.P2CRaceHits == 0 {
		t.Fatalf("counters not moving: race=%d rr=%d", s.P2CRaceHits, s.P2CRRHits)
	}
	if s.P2CRaceHits+s.P2CRRHits+s.P2CFallbacks != 100 {
		t.Fatalf("counters must sum to allocations: %d+%d+%d != 100", s.P2CRaceHits, s.P2CRRHits, s.P2CFallbacks)
	}
	if s.PollSliceLen != s.FreeIPCount {
		t.Fatalf("PollSliceLen %d != FreeIPCount %d", s.PollSliceLen, s.FreeIPCount)
	}
}

// LRU 语义：竞速选中者被转到队尾（下一次 RR 保底不会重复拿它）。
func TestP2C_WinnerRotatesToBack(t *testing.T) {
	p := fillPool(16, func(i int) int64 {
		if i == 3 {
			return 10
		}
		return 999
	})
	cfg := raceOnly(4) // D=4 提高 3 号被抽中概率
	var winner string
	for c := 0; c < 50; c++ {
		if ip, _ := p.AcquireP2CPollingIP(cfg); ip == "10.0.0.3" {
			winner = ip
			break
		}
	}
	if winner == "" {
		t.Skip("winner never sampled in 50 draws (improbable)")
	}
	p.mu.Lock()
	backIP := p.freeList.Back().Value.(*ipEntry).ip
	p.mu.Unlock()
	if backIP != "10.0.0.3" {
		t.Fatalf("winner should be at back of freeList, back is %s", backIP)
	}
}
