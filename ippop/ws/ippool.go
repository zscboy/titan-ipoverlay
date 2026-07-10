package ws

import (
	"container/list"
	"fmt"
	"math"
	"math/rand/v2"
	"sort"
	"sync"
	"time"
	"titan-ipoverlay/ippop/types"

	"github.com/zeromicro/go-zero/core/logx"
)

// ipEntry tracks an IP and its associated tunnels
type ipEntry struct {
	ip             string
	tunnels        map[string]*Tunnel // nodeID -> Tunnel
	element        *list.Element      // Pointer to position in freeList
	localIPElement *list.Element      // Pointer to position in localIPFreeList[localIP]
	regionElement  *list.Element      // Pointer to position in regionFreeList[region]
	localIP        string             // The local IP (NIC IP) this entry is associated with
	region         string             // The region this IP belongs to
	assignedNodeID string             // The nodeID currently given out by AcquireIP
	isBlacklisted  bool               // New: tracks if this IP is in blacklist
	sliceIdx       int                // Index in IPPool.pollSlice; -1 = not in free pool
}

// IPPool manages a pool of unique exit IPs from connected nodes
type IPPool struct {
	mu sync.Mutex

	allIPs          map[string]*ipEntry   // ip -> entry
	freeList        *list.List            // List of *ipEntry (available IPs) (Global pool)
	localIPFreeList map[string]*list.List // localIP -> List of *ipEntry (Line-specific pools)
	regionFreeList  map[string]*list.List // region -> List of *ipEntry (Region-specific pools)
	localIPRR       uint64                // Round-robin counter for lines
	blacklistCount  int                   // New: count of IPs currently blacklisted in the pool
	assignedCount   int                   // New: count of IPs currently assigned
	tunnelCount     int                   // total count of tunnels in the pool
	lineNodes       map[string]int        // Real-time: LocalIP -> tunnel count
	pollSlice       []*ipEntry            // Companion index of freeList for O(1) random sampling (P2C)

	p2cCounter   uint64 // P2C 保底轮询取模计数器（p.mu 保护）
	p2cRaceHits  uint64 // 竞速路径选中次数（可观测性，p.mu 保护）
	p2cRRHits    uint64 // 保底/薄池轮询路径次数
	p2cFallbacks uint64 // 竞速全弃权退化次数（冷启动/陈旧/超容）
}

type PoolStats struct {
	TotalIPCount     int
	FreeIPCount      int
	BlacklistIPCount int
	AssignedIPCount  int
	TunnelCount      int
	LineNodes        map[string]int // LineID (LocalIP) -> NodeCount in free list
	RegionNodes      map[string]int // Region -> NodeCount in free list
	PollSliceLen     int            // P2C companion index length (must equal FreeIPCount)
	P2CRaceHits      uint64         // cumulative P2C race-path selections
	P2CRRHits        uint64         // cumulative backstop/thin-pool front-polling selections
	P2CFallbacks     uint64         // cumulative all-candidates-sat-out degradations
}

func NewIPPool() *IPPool {
	return &IPPool{
		allIPs:          make(map[string]*ipEntry),
		freeList:        list.New(),
		localIPFreeList: make(map[string]*list.List),
		regionFreeList:  make(map[string]*list.List),
		lineNodes:       make(map[string]int),
	}
}

func (p *IPPool) addToFreePool(entry *ipEntry) {
	if entry.element == nil {
		entry.element = p.freeList.PushBack(entry)
		entry.sliceIdx = len(p.pollSlice)
		p.pollSlice = append(p.pollSlice, entry)
	}
	if entry.localIPElement == nil {
		l, ok := p.localIPFreeList[entry.localIP]
		if !ok {
			l = list.New()
			p.localIPFreeList[entry.localIP] = l
		}
		entry.localIPElement = l.PushBack(entry)
	}
	if entry.region != "" && entry.regionElement == nil {
		l, ok := p.regionFreeList[entry.region]
		if !ok {
			l = list.New()
			p.regionFreeList[entry.region] = l
		}
		entry.regionElement = l.PushBack(entry)
	}
}

func (p *IPPool) removeFromFreePool(entry *ipEntry) {
	if entry.element != nil {
		p.freeList.Remove(entry.element)
		entry.element = nil

		idx := entry.sliceIdx
		if idx < 0 || idx >= len(p.pollSlice) || p.pollSlice[idx] != entry {
			// Should never happen; degrade to a linear scan rather than panicking the hot path.
			logx.Errorf("IPPool.pollSlice index corrupted for %s (idx=%d), rescanning", entry.ip, idx)
			idx = -1
			for i, e := range p.pollSlice {
				if e == entry {
					idx = i
					break
				}
			}
		}
		if idx >= 0 {
			last := len(p.pollSlice) - 1
			p.pollSlice[idx] = p.pollSlice[last]
			p.pollSlice[idx].sliceIdx = idx
			p.pollSlice[last] = nil
			p.pollSlice = p.pollSlice[:last]
		} else {
			logx.Errorf("IPPool.pollSlice: entry %s not found during removal (already absent from slice)", entry.ip)
		}
		entry.sliceIdx = -1
	}
	if entry.localIPElement != nil {
		if l, ok := p.localIPFreeList[entry.localIP]; ok {
			l.Remove(entry.localIPElement)
		}
		entry.localIPElement = nil
	}
	if entry.regionElement != nil {
		if l, ok := p.regionFreeList[entry.region]; ok {
			l.Remove(entry.regionElement)
		}
		entry.regionElement = nil
	}
}

// AddTunnel adds a tunnel to the pool.
func (p *IPPool) AddTunnel(t *Tunnel, isBlacklisted bool) {
	p.mu.Lock()
	defer p.mu.Unlock()

	ip := t.opts.IP
	nodeID := t.opts.Id
	localIP := t.opts.LocalIP
	region := t.opts.Region

	entry, ok := p.allIPs[ip]
	if !ok {
		entry = &ipEntry{
			ip:            ip,
			tunnels:       make(map[string]*Tunnel),
			isBlacklisted: isBlacklisted,
			localIP:       localIP,
			region:        region,
			sliceIdx:      -1,
		}
		p.allIPs[ip] = entry
		if isBlacklisted {
			p.blacklistCount++
		}

		// New IP starts as free if not blacklisted
		if !isBlacklisted {
			p.addToFreePool(entry)
		}
	} else {
		// If entry already exists, update blacklist status if it changed
		if !entry.isBlacklisted && isBlacklisted {
			entry.isBlacklisted = true
			p.blacklistCount++
			p.removeFromFreePool(entry)
		} else if entry.isBlacklisted && !isBlacklisted {
			entry.isBlacklisted = false
			p.blacklistCount--
			if entry.element == nil && entry.assignedNodeID == "" {
				p.addToFreePool(entry)
			}
		}
	}

	entry.tunnels[nodeID] = t
	p.tunnelCount++
	p.lineNodes[localIP]++
}

// RemoveTunnel removes a tunnel. If it was the last tunnel for an IP, the IP is removed.
func (p *IPPool) RemoveTunnel(t *Tunnel) {
	p.mu.Lock()
	defer p.mu.Unlock()

	entry, ok := p.allIPs[t.opts.IP]
	if !ok {
		return
	}

	delete(entry.tunnels, t.opts.Id)
	p.tunnelCount--
	p.lineNodes[t.opts.LocalIP]--

	// If the tunnel being removed was the one assigned to a session
	if entry.assignedNodeID == t.opts.Id {
		entry.assignedNodeID = ""
		p.assignedCount--
		// If the IP was Busy but still has other tunnels,
		// return it to free lists so it can be re-acquired (avoid leak)
		if entry.element == nil && len(entry.tunnels) > 0 {
			p.addToFreePool(entry)
		}
	}

	// If no more tunnels for this IP, remove the IP from the pool
	if len(entry.tunnels) == 0 {
		p.removeFromFreePool(entry)
		if entry.isBlacklisted {
			p.blacklistCount--
		}
		delete(p.allIPs, t.opts.IP)
	}
}

// ActivateIP marks an IP as not blacklisted and returns it to the free list if possible.
func (p *IPPool) ActivateIP(ip string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	entry, ok := p.allIPs[ip]
	if !ok || !entry.isBlacklisted {
		return
	}

	entry.isBlacklisted = false
	p.blacklistCount--

	// If it has tunnels and is not assigned, it should be in free lists
	if len(entry.tunnels) > 0 && entry.element == nil && entry.assignedNodeID == "" {
		p.addToFreePool(entry)
	}
}

// DeactivateIP marks an IP as blacklisted and removes it from the free list.
func (p *IPPool) DeactivateIP(ip string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	entry, ok := p.allIPs[ip]
	if !ok || entry.isBlacklisted {
		// Even if not in allIPs, we might want to track the state if it connects later?
		// For now, let's assume we only track online IPs.
		return
	}

	entry.isBlacklisted = true
	p.blacklistCount++

	p.removeFromFreePool(entry)
}

// AcquireIP is the unified entry point for IP allocation.
// It prioritizes region-based allocation if a region is specified,
// otherwise balances across PPPoE lines if multiple are available.
// If none of the above apply, it falls back to standard FIFO allocation.
func (p *IPPool) AcquireIP(region string) (string, *Tunnel) {
	p.mu.Lock()
	defer p.mu.Unlock()

	// 1. If region is specified, try region list
	if region != "" {
		if l, ok := p.regionFreeList[region]; ok && l.Len() > 0 {
			return p.acquireFromListLocked(l)
		}
	}

	// 2. If multiple local IPs detected, use Line strategy (balanced)
	if len(p.localIPFreeList) > 1 {
		return p.acquireByLineLocked()
	}

	// 3. Fallback to standard Acquire (FIFO)
	return p.acquireFromListLocked(p.freeList)
}

func (p *IPPool) acquireFromListLocked(l *list.List) (string, *Tunnel) {
	element := l.Front()
	if element == nil {
		return "", nil
	}
	entry := element.Value.(*ipEntry)
	p.removeFromFreePool(entry)

	for id, t := range entry.tunnels {
		entry.assignedNodeID = id
		p.assignedCount++
		return entry.ip, t
	}
	return "", nil
}

func (p *IPPool) acquireByLineLocked() (string, *Tunnel) {
	if len(p.localIPFreeList) == 0 {
		return "", nil
	}

	localIPs := make([]string, 0, len(p.localIPFreeList))
	for ip := range p.localIPFreeList {
		localIPs = append(localIPs, ip)
	}
	sort.Strings(localIPs)

	startIdx := int(p.localIPRR % uint64(len(localIPs)))
	p.localIPRR++

	for i := 0; i < len(localIPs); i++ {
		idx := (startIdx + i) % len(localIPs)
		localIP := localIPs[idx]
		l := p.localIPFreeList[localIP]

		if l.Len() > 0 {
			element := l.Front()
			entry := element.Value.(*ipEntry)

			p.removeFromFreePool(entry)

			for id, t := range entry.tunnels {
				entry.assignedNodeID = id
				p.assignedCount++
				return entry.ip, t
			}
		}
	}

	return "", nil
}

// ReleaseIP returns an IP to the free list if it still has active nodes and is not blacklisted.
func (p *IPPool) ReleaseIP(ip string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	entry, ok := p.allIPs[ip]
	if !ok {
		return
	}

	// Always clear assignment
	if entry.assignedNodeID != "" {
		entry.assignedNodeID = ""
		p.assignedCount--
	}

	// return to free lists only if not blacklisted and has active tunnels
	if entry.element == nil && len(entry.tunnels) > 0 && !entry.isBlacklisted {
		p.addToFreePool(entry)
	}
}

func (p *IPPool) GetTunnelsByIP(ip string) []*Tunnel {
	p.mu.Lock()
	defer p.mu.Unlock()

	entry, ok := p.allIPs[ip]
	if !ok {
		return nil
	}

	tunnels := make([]*Tunnel, 0, len(entry.tunnels))
	for _, t := range entry.tunnels {
		tunnels = append(tunnels, t)
	}
	return tunnels
}

func (p *IPPool) GetIPAssignmentStatus(ip string) (exists bool, isAssigned bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	entry, ok := p.allIPs[ip]
	if !ok {
		return false, false
	}
	return true, entry.assignedNodeID != ""
}

func (p *IPPool) IsIPDeactivated(ip string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	entry, ok := p.allIPs[ip]
	if !ok {
		return false
	}
	return entry.isBlacklisted
}

func (p *IPPool) GetPoolStats() PoolStats {
	p.mu.Lock()
	defer p.mu.Unlock()

	stats := make(map[string]int)
	for k, v := range p.lineNodes {
		stats[k] = v
	}

	return PoolStats{
		TotalIPCount:     len(p.allIPs),
		FreeIPCount:      p.freeList.Len(),
		BlacklistIPCount: p.blacklistCount,
		AssignedIPCount:  p.assignedCount,
		TunnelCount:      p.tunnelCount,
		LineNodes:        stats,
		// RegionNodes:      regionNodes,
		PollSliceLen: len(p.pollSlice),
		P2CRaceHits:  p.p2cRaceHits,
		P2CRRHits:    p.p2cRRHits,
		P2CFallbacks: p.p2cFallbacks,
	}
}

// GetFreeIPsFromTail retrieves free IPs from the tail of the free list.
func (p *IPPool) GetFreeIPsFromTail(count int) []types.FreeIPInfo {
	p.mu.Lock()
	defer p.mu.Unlock()

	if count <= 0 {
		return nil
	}

	res := make([]types.FreeIPInfo, 0, count)
	for e := p.freeList.Back(); e != nil && len(res) < count; e = e.Prev() {
		entry := e.Value.(*ipEntry)
		nodeIDs := make([]string, 0, len(entry.tunnels))
		for nodeID := range entry.tunnels {
			nodeIDs = append(nodeIDs, nodeID)
		}
		res = append(res, types.FreeIPInfo{
			IP:      entry.ip,
			NodeIDs: nodeIDs,
		})
	}
	return res
}

// GetFreeIPsFromHead retrieves free IPs from the head of the free list.
func (p *IPPool) GetFreeIPsFromHead(count int) []types.FreeIPInfo {
	p.mu.Lock()
	defer p.mu.Unlock()

	if count <= 0 {
		return nil
	}

	res := make([]types.FreeIPInfo, 0, count)
	for e := p.freeList.Front(); e != nil && len(res) < count; e = e.Next() {
		entry := e.Value.(*ipEntry)
		nodeIDs := make([]string, 0, len(entry.tunnels))
		for nodeID := range entry.tunnels {
			nodeIDs = append(nodeIDs, nodeID)
		}
		res = append(res, types.FreeIPInfo{
			IP:      entry.ip,
			NodeIDs: nodeIDs,
		})
	}
	return res
}

// AcquirePollingIP picks an IP for polling mode in O(1) and rotates it to the back.
func (p *IPPool) AcquirePollingIP() (string, *Tunnel) {
	p.mu.Lock()
	defer p.mu.Unlock()

	return p.acquirePollingFrontLocked()
}

// rotateEntryToBackLocked moves an entry to the back of all its free lists to keep
// LRU/round-robin order. Caller must hold p.mu.
func (p *IPPool) rotateEntryToBackLocked(entry *ipEntry) {
	if entry.element != nil {
		p.freeList.MoveToBack(entry.element)
	}
	if entry.localIPElement != nil {
		if l, ok := p.localIPFreeList[entry.localIP]; ok {
			l.MoveToBack(entry.localIPElement)
		}
	}
	if entry.regionElement != nil {
		if l, ok := p.regionFreeList[entry.region]; ok {
			l.MoveToBack(entry.regionElement)
		}
	}
}

// acquirePollingFrontLocked takes the front IP, rotates it to the back, and returns one
// of its tunnels. This is the original O(1) round-robin polling. Caller must hold p.mu.
func (p *IPPool) acquirePollingFrontLocked() (string, *Tunnel) {
	element := p.freeList.Front()
	if element == nil {
		return "", nil
	}
	entry := element.Value.(*ipEntry)
	p.rotateEntryToBackLocked(entry)
	for _, t := range entry.tunnels {
		return entry.ip, t
	}
	return "", nil
}

// p2cDelayStaleMs: delay 样本超过此毫秒数未刷新即视为陈旧（忙隧道被动 keepalive
// 会冻结 delay），该候选在竞速中弃权，由保底轮询喂养。
const p2cDelayStaleMs = 60_000

// P2CParams carries the (already sanitized) knobs for AcquireP2CPollingIP.
type P2CParams struct {
	Depth          int // D: 随机采样候选数；<2 时调用方不应走本方法
	RREvery        int // R: 每第 R 个请求强制走轮询队首保底；<2 = 每请求都保底(等价原混播)
	LambdaMs       int // λ: 每个在途会话折算的毫秒惩罚
	MinPool        int // 薄池护栏：空闲 IP 少于此数整体退化为轮询
	MaxBoxSessions int // 单盒在途硬上限，0=关
}

// sanitizeP2CParams clamps unsafe P2C config into safe values and reports what
// changed ("" = nothing). LambdaMs < 50 with Depth >= 2 disables P2C entirely:
// load-blind racing (lambda=0) doubled 1007 disconnects in the loaded simulation,
// so that knob must not be mis-set into a self-destruct position.
func sanitizeP2CParams(cfg P2CParams) (P2CParams, string) {
	warn := ""
	if cfg.Depth >= 2 && cfg.LambdaMs < 50 {
		warn += fmt.Sprintf("PollingP2CLoadPenaltyMs=%d <50 is unsafe, P2C disabled; ", cfg.LambdaMs)
		cfg.Depth = 0
	}
	if cfg.Depth > 4 {
		warn += fmt.Sprintf("PollingP2CDepth=%d clamped to 4; ", cfg.Depth)
		cfg.Depth = 4
	}
	if cfg.RREvery > 10 {
		// R 红线:过大时新盒/未测量盒只能靠保底喂养,饥饿上界 n×R 失去意义。
		warn += fmt.Sprintf("PollingP2CRRInterval=%d clamped to 10; ", cfg.RREvery)
		cfg.RREvery = 10
	}
	if cfg.RREvery < 1 {
		cfg.RREvery = 5
	}
	if cfg.MinPool < 2 {
		warn += fmt.Sprintf("PollingP2CMinPool=%d raised to 16; ", cfg.MinPool)
		cfg.MinPool = 16
	}
	return cfg, warn
}

// AcquireP2CPollingIP picks an exit IP for polling mode with load-aware
// power-of-D-choices: sample Depth random free entries (with replacement),
// score each tunnel as delay + LambdaMs*inflight, take the lowest. Every
// RREvery-th request takes the freeList front instead. Because every winner
// is rotated to the back, the freeList stays ordered by least-recently-
// selected, so the backstop always feeds the most starved box: any pooled IP
// is selected at least once per poolSize*RREvery calls (bounded starvation).
// Candidates with no RTT sample (delay<=0), a stale sample, or inflight >=
// MaxBoxSessions sit the race out; if all sampled candidates sit out, the
// call degrades to plain front polling. Never worse than plain polling.
func (p *IPPool) AcquireP2CPollingIP(cfg P2CParams) (string, *Tunnel) {
	p.mu.Lock()
	defer p.mu.Unlock()

	n := len(p.pollSlice)
	if n == 0 {
		return "", nil
	}
	p.p2cCounter++
	if n < cfg.MinPool || cfg.RREvery < 2 || p.p2cCounter%uint64(cfg.RREvery) == 0 {
		p.p2cRRHits++
		return p.acquirePollingFrontLocked()
	}

	nowMs := time.Now().UnixMilli()
	var bestEntry *ipEntry
	var bestTun *Tunnel
	bestScore := int64(math.MaxInt64)
	for j := 0; j < cfg.Depth; j++ {
		e := p.pollSlice[rand.IntN(n)]
		for _, t := range e.tunnels {
			dl := t.delay.Load()
			if dl <= 0 {
				continue // no RTT sample yet (cold start): sit out, backstop feeds it
			}
			if nowMs-t.lastPongAt.Load() > p2cDelayStaleMs {
				continue // frozen delay (busy tunnel suppresses pings): distrust it
			}
			inflight := int64(t.proxys.Count())
			if cfg.MaxBoxSessions > 0 && inflight >= int64(cfg.MaxBoxSessions) {
				continue // hard per-box concurrency cap
			}
			score := dl + int64(cfg.LambdaMs)*inflight
			if score < bestScore {
				bestScore, bestEntry, bestTun = score, e, t
			}
		}
	}
	if bestTun == nil {
		p.p2cFallbacks++
		return p.acquirePollingFrontLocked()
	}
	p.p2cRaceHits++
	p.rotateEntryToBackLocked(bestEntry)
	return bestEntry.ip, bestTun
}
