package ws

import (
	"container/list"
	"sort"
	"sync"
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
}

type PoolStats struct {
	TotalIPCount     int
	FreeIPCount      int
	BlacklistIPCount int
	AssignedIPCount  int
	TunnelCount      int
	LineNodes        map[string]int // LineID (LocalIP) -> NodeCount in free list
	RegionNodes      map[string]int // Region -> NodeCount in free list
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
	}
}

// FreeIPInfo contains IP and its associated NodeIDs
type FreeIPInfo struct {
	IP      string   `json:"ip"`
	NodeIDs []string `json:"node_ids"`
}

// GetFreeIPsFromTail retrieves free IPs from the tail of the free list.
func (p *IPPool) GetFreeIPsFromTail(count int) []FreeIPInfo {
	p.mu.Lock()
	defer p.mu.Unlock()

	if count <= 0 {
		return nil
	}

	res := make([]FreeIPInfo, 0, count)
	for e := p.freeList.Back(); e != nil && len(res) < count; e = e.Prev() {
		entry := e.Value.(*ipEntry)
		nodeIDs := make([]string, 0, len(entry.tunnels))
		for nodeID := range entry.tunnels {
			nodeIDs = append(nodeIDs, nodeID)
		}
		res = append(res, FreeIPInfo{
			IP:      entry.ip,
			NodeIDs: nodeIDs,
		})
	}
	return res
}

// GetFreeIPsFromHead retrieves free IPs from the head of the free list.
func (p *IPPool) GetFreeIPsFromHead(count int) []FreeIPInfo {
	p.mu.Lock()
	defer p.mu.Unlock()

	if count <= 0 {
		return nil
	}

	res := make([]FreeIPInfo, 0, count)
	for e := p.freeList.Front(); e != nil && len(res) < count; e = e.Next() {
		entry := e.Value.(*ipEntry)
		nodeIDs := make([]string, 0, len(entry.tunnels))
		for nodeID := range entry.tunnels {
			nodeIDs = append(nodeIDs, nodeID)
		}
		res = append(res, FreeIPInfo{
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

	element := p.freeList.Front()
	if element == nil {
		return "", nil
	}

	entry := element.Value.(*ipEntry)

	// Rotate: move to the back of all free lists to maintain LRU/Round-robin order.
	p.freeList.MoveToBack(element)
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

	for _, t := range entry.tunnels {
		return entry.ip, t
	}

	return "", nil
}
