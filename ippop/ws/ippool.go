package ws

import (
	"container/list"
	"sort"
	"sync"
	"sync/atomic"
)

// ipEntry tracks an IP and its associated tunnels
type ipEntry struct {
	ip             string
	tunnels        map[string]*Tunnel // nodeID -> Tunnel
	element        *list.Element      // Pointer to position in freeList
<<<<<<< HEAD
	localIPElement *list.Element      // Pointer to position in localIPFreeList[localIP]
	regionElement  *list.Element      // Pointer to position in regionFreeList[region]
	localIP        string             // The local IP (NIC IP) this entry is associated with
	region         string             // The region this IP belongs to
=======
	lineID         string             // Which physical line this IP is currently grouped in
>>>>>>> 9e3fb41 (feat(ippop): implement dynamic multi-line bandwidth balancing in IPPool)
	assignedNodeID string             // The nodeID currently given out by AcquireIP
	isBlacklisted  bool               // tracks if this IP is in blacklist
}

// IPPool manages a pool of unique exit IPs from connected nodes
type IPPool struct {
	mu sync.Mutex

<<<<<<< HEAD
	allIPs          map[string]*ipEntry   // ip -> entry
	freeList        *list.List            // List of *ipEntry (available IPs) (Global pool)
	localIPFreeList map[string]*list.List // localIP -> List of *ipEntry (Line-specific pools)
	regionFreeList  map[string]*list.List // region -> List of *ipEntry (Region-specific pools)
	localIPRR       uint64                // Round-robin counter for lines
	blacklistCount  int                   // New: count of IPs currently blacklisted in the pool
	assignedCount   int                   // New: count of IPs currently assigned
	tunnelCount     int                   // total count of tunnels in the pool
	lineNodes       map[string]int        // Real-time: LocalIP -> tunnel count
=======
	allIPs         map[string]*ipEntry   // ip -> entry
	freeLists      map[string]*list.List // lineID (LocalIP) -> free list
	activeLines    []string              // Sorted list of active lines for round-robin
	lineStats      map[string]int        // Real-time: LocalIP -> tunnel count
	nextLineIdx    uint32                // Atomic index for round-robin
	blacklistCount int                   // count of IPs currently blacklisted
	assignedCount  int                   // count of IPs currently assigned
	tunnelCount    int                   // total count of tunnels
>>>>>>> 9e3fb41 (feat(ippop): implement dynamic multi-line bandwidth balancing in IPPool)
}

type PoolStats struct {
	TotalIPCount     int
	FreeIPCount      int
	BlacklistIPCount int
	AssignedIPCount  int
	TunnelCount      int
<<<<<<< HEAD
	LineNodes        map[string]int // LineID (LocalIP) -> NodeCount in free list
	RegionNodes      map[string]int // Region -> NodeCount in free list
=======
	LineStats        map[string]int // LocalIP -> tunnel count
>>>>>>> 4683969 (Refactor LineStats in IPPool to  by maintaining real-time counters during tunnel add/remove operations.)
}

func NewIPPool() *IPPool {
	return &IPPool{
<<<<<<< HEAD
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
=======
		allIPs:    make(map[string]*ipEntry),
		freeLists: make(map[string]*list.List),
<<<<<<< HEAD
>>>>>>> 9e3fb41 (feat(ippop): implement dynamic multi-line bandwidth balancing in IPPool)
=======
		lineStats: make(map[string]int),
>>>>>>> 4683969 (Refactor LineStats in IPPool to  by maintaining real-time counters during tunnel add/remove operations.)
	}
}

// updateActiveLines refreshes the sorted list of line IDs
func (p *IPPool) updateActiveLines() {
	lines := make([]string, 0, len(p.freeLists))
	for k := range p.freeLists {
		lines = append(lines, k)
	}
	sort.Strings(lines)
	p.activeLines = lines
}

// AddTunnel adds a tunnel to the pool.
func (p *IPPool) AddTunnel(t *Tunnel, isBlacklisted bool) {
	p.mu.Lock()
	defer p.mu.Unlock()

	ip := t.opts.IP
	nodeID := t.opts.Id
<<<<<<< HEAD
	localIP := t.opts.LocalIP
	region := t.opts.Region
=======
	lineID := t.opts.LocalIP
>>>>>>> 9e3fb41 (feat(ippop): implement dynamic multi-line bandwidth balancing in IPPool)

	entry, ok := p.allIPs[ip]
	if !ok {
		entry = &ipEntry{
			ip:            ip,
			tunnels:       make(map[string]*Tunnel),
			lineID:        lineID,
			isBlacklisted: isBlacklisted,
			localIP:       localIP,
			region:        region,
		}
		p.allIPs[ip] = entry
		if isBlacklisted {
			p.blacklistCount++
		}
<<<<<<< HEAD

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
=======
	} else {
		if !entry.isBlacklisted && isBlacklisted {
			entry.isBlacklisted = true
			p.blacklistCount++
			if entry.element != nil {
				p.freeLists[entry.lineID].Remove(entry.element)
				entry.element = nil
			}
		} else if entry.isBlacklisted && !isBlacklisted {
			entry.isBlacklisted = false
			p.blacklistCount--
<<<<<<< HEAD
			// No need to add here, the unified check below (line 98) will handle it
>>>>>>> 9e3fb41 (feat(ippop): implement dynamic multi-line bandwidth balancing in IPPool)
=======
>>>>>>> 4683969 (Refactor LineStats in IPPool to  by maintaining real-time counters during tunnel add/remove operations.)
		}
	}

	entry.tunnels[nodeID] = t
	p.tunnelCount++
<<<<<<< HEAD
<<<<<<< HEAD
	p.lineNodes[localIP]++
=======
=======
	p.lineStats[lineID]++
>>>>>>> 4683969 (Refactor LineStats in IPPool to  by maintaining real-time counters during tunnel add/remove operations.)

	if !entry.isBlacklisted && entry.assignedNodeID == "" && entry.element == nil {
		p.addToFreeList(entry)
	}
>>>>>>> 9e3fb41 (feat(ippop): implement dynamic multi-line bandwidth balancing in IPPool)
}

func (p *IPPool) addToFreeList(entry *ipEntry) {
	if _, exists := p.freeLists[entry.lineID]; !exists {
		p.freeLists[entry.lineID] = list.New()
		p.updateActiveLines()
	}
	entry.element = p.freeLists[entry.lineID].PushBack(entry)
}

// RemoveTunnel removes a tunnel.
func (p *IPPool) RemoveTunnel(t *Tunnel) {
	p.mu.Lock()
	defer p.mu.Unlock()

	entry, ok := p.allIPs[t.opts.IP]
	if !ok {
		return
	}

	delete(entry.tunnels, t.opts.Id)
	p.tunnelCount--
<<<<<<< HEAD
	p.lineNodes[t.opts.LocalIP]--
=======
	p.lineStats[t.opts.LocalIP]--
>>>>>>> 4683969 (Refactor LineStats in IPPool to  by maintaining real-time counters during tunnel add/remove operations.)

	if entry.assignedNodeID == t.opts.Id {
		entry.assignedNodeID = ""
		p.assignedCount--
<<<<<<< HEAD
		// If the IP was Busy but still has other tunnels,
		// return it to free lists so it can be re-acquired (avoid leak)
		if entry.element == nil && len(entry.tunnels) > 0 {
			p.addToFreePool(entry)
=======
		if entry.element == nil && len(entry.tunnels) > 0 {
			p.addToFreeList(entry)
>>>>>>> 9e3fb41 (feat(ippop): implement dynamic multi-line bandwidth balancing in IPPool)
		}
	}

	if len(entry.tunnels) == 0 {
<<<<<<< HEAD
		p.removeFromFreePool(entry)
=======
		if entry.element != nil {
			p.freeLists[entry.lineID].Remove(entry.element)
		}
>>>>>>> 9e3fb41 (feat(ippop): implement dynamic multi-line bandwidth balancing in IPPool)
		if entry.isBlacklisted {
			p.blacklistCount--
		}
		delete(p.allIPs, t.opts.IP)
	}
}

<<<<<<< HEAD
<<<<<<< HEAD
// RemoveIP removes the entire IP entry from the pool, regardless of tunnels.
// func (p *IPPool) RemoveIP(ip string) {
// 	p.mu.Lock()
// 	defer p.mu.Unlock()
=======
// RemoveIP removes the entire IP entry.
func (p *IPPool) RemoveIP(ip string) {
	p.mu.Lock()
	defer p.mu.Unlock()
>>>>>>> 9e3fb41 (feat(ippop): implement dynamic multi-line bandwidth balancing in IPPool)

// 	entry, ok := p.allIPs[ip]
// 	if !ok {
// 		return
// 	}

<<<<<<< HEAD
// 	p.removeFromFreePool(entry)
// 	if entry.isBlacklisted {
// 		p.blacklistCount--
// 	}
// 	if entry.assignedNodeID != "" {
// 		p.assignedCount--
// 	}
// 	delete(p.allIPs, ip)
// }
=======
	if entry.element != nil {
		p.freeLists[entry.lineID].Remove(entry.element)
	}
	if entry.isBlacklisted {
		p.blacklistCount--
	}
	if entry.assignedNodeID != "" {
		p.assignedCount--
	}
	delete(p.allIPs, ip)
}
>>>>>>> 9e3fb41 (feat(ippop): implement dynamic multi-line bandwidth balancing in IPPool)

=======
>>>>>>> 4683969 (Refactor LineStats in IPPool to  by maintaining real-time counters during tunnel add/remove operations.)
// ActivateIP marks an IP as not blacklisted.
func (p *IPPool) ActivateIP(ip string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	entry, ok := p.allIPs[ip]
	if !ok || !entry.isBlacklisted {
		return
	}

	entry.isBlacklisted = false
	p.blacklistCount--

<<<<<<< HEAD
	// If it has tunnels and is not assigned, it should be in free lists
	if len(entry.tunnels) > 0 && entry.element == nil && entry.assignedNodeID == "" {
		p.addToFreePool(entry)
=======
	if len(entry.tunnels) > 0 && entry.element == nil && entry.assignedNodeID == "" {
		p.addToFreeList(entry)
>>>>>>> 9e3fb41 (feat(ippop): implement dynamic multi-line bandwidth balancing in IPPool)
	}
}

// DeactivateIP marks an IP as blacklisted.
func (p *IPPool) DeactivateIP(ip string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	entry, ok := p.allIPs[ip]
	if !ok || entry.isBlacklisted {
		return
	}

	entry.isBlacklisted = true
	p.blacklistCount++

<<<<<<< HEAD
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
=======
	if entry.element != nil {
		p.freeLists[entry.lineID].Remove(entry.element)
		entry.element = nil
	}
}

// AcquireIP picks a free IP using round-robin across lines.
func (p *IPPool) AcquireIP() (string, *Tunnel) {
	p.mu.Lock()
	defer p.mu.Unlock()

	numLines := len(p.activeLines)
	if numLines == 0 {
		return "", nil
	}

	startIdx := atomic.AddUint32(&p.nextLineIdx, 1) % uint32(numLines)

	for i := 0; i < numLines; i++ {
		lineIdx := (int(startIdx) + i) % numLines
		lineID := p.activeLines[lineIdx]

		fl := p.freeLists[lineID]
		element := fl.Front()
		if element == nil {
			continue
		}

		entry := element.Value.(*ipEntry)
		fl.Remove(element)
		entry.element = nil

		// Prefer a tunnel on this physical line
		for id, t := range entry.tunnels {
			if t.opts.LocalIP == lineID {
				entry.assignedNodeID = id
				p.assignedCount++
				return entry.ip, t
			}
		}

		// Fallback: any tunnel
		for id, t := range entry.tunnels {
			entry.assignedNodeID = id
			p.assignedCount++
			return entry.ip, t
		}
>>>>>>> 9e3fb41 (feat(ippop): implement dynamic multi-line bandwidth balancing in IPPool)
	}
	return "", nil
}

<<<<<<< HEAD
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

=======
>>>>>>> 9e3fb41 (feat(ippop): implement dynamic multi-line bandwidth balancing in IPPool)
	return "", nil
}

// ReleaseIP returns an IP to its line's free list.
func (p *IPPool) ReleaseIP(ip string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	entry, ok := p.allIPs[ip]
	if !ok {
		return
	}

	if entry.assignedNodeID != "" {
		entry.assignedNodeID = ""
		p.assignedCount--
	}

<<<<<<< HEAD
	// return to free lists only if not blacklisted and has active tunnels
	if entry.element == nil && len(entry.tunnels) > 0 && !entry.isBlacklisted {
		p.addToFreePool(entry)
=======
	if entry.element == nil && len(entry.tunnels) > 0 && !entry.isBlacklisted {
		p.addToFreeList(entry)
>>>>>>> 9e3fb41 (feat(ippop): implement dynamic multi-line bandwidth balancing in IPPool)
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

<<<<<<< HEAD
	stats := make(map[string]int)
	for k, v := range p.lineNodes {
		stats[k] = v
=======
	totalFree := 0
	for _, fl := range p.freeLists {
		totalFree += fl.Len()
>>>>>>> 9e3fb41 (feat(ippop): implement dynamic multi-line bandwidth balancing in IPPool)
	}

	// O(1) Copy: Since number of lines is very small (usually <= 20)
	stats := make(map[string]int)
	for k, v := range p.lineStats {
		stats[k] = v
	}

	return PoolStats{
		TotalIPCount:     len(p.allIPs),
		FreeIPCount:      totalFree,
		BlacklistIPCount: p.blacklistCount,
		AssignedIPCount:  p.assignedCount,
		TunnelCount:      p.tunnelCount,
<<<<<<< HEAD
		LineNodes:        stats,
		// RegionNodes:      regionNodes,
=======
		LineStats:        stats,
>>>>>>> 4683969 (Refactor LineStats in IPPool to  by maintaining real-time counters during tunnel add/remove operations.)
	}
}

// FreeIPInfo contains IP and its associated NodeIDs
type FreeIPInfo struct {
	IP      string   `json:"ip"`
	NodeIDs []string `json:"node_ids"`
}

// GetFreeIPsFromTail retrieves free IPs using round-robin across all active lines, from the tail.
func (p *IPPool) GetFreeIPsFromTail(count int) []FreeIPInfo {
	p.mu.Lock()
	defer p.mu.Unlock()

	if count <= 0 || len(p.activeLines) == 0 {
		return nil
	}

	res := make([]FreeIPInfo, 0, count)
	// Track current element for each line
	iters := make([]*list.Element, len(p.activeLines))
	for i, lineID := range p.activeLines {
		iters[i] = p.freeLists[lineID].Back()
	}

	for len(res) < count {
		anyAdded := false
		for i := range iters {
			if len(res) >= count {
				break
			}
			if iters[i] != nil {
				entry := iters[i].Value.(*ipEntry)
				nodeIDs := make([]string, 0, len(entry.tunnels))
				for nodeID := range entry.tunnels {
					nodeIDs = append(nodeIDs, nodeID)
				}
				res = append(res, FreeIPInfo{
					IP:      entry.ip,
					NodeIDs: nodeIDs,
				})
				iters[i] = iters[i].Prev()
				anyAdded = true
			}
		}
		if !anyAdded {
			break
		}
	}
	return res
}

// GetFreeIPsFromHead retrieves free IPs using round-robin across all active lines, from the head.
func (p *IPPool) GetFreeIPsFromHead(count int) []FreeIPInfo {
	p.mu.Lock()
	defer p.mu.Unlock()

	if count <= 0 || len(p.activeLines) == 0 {
		return nil
	}

	res := make([]FreeIPInfo, 0, count)
	// Track current element for each line
	iters := make([]*list.Element, len(p.activeLines))
	for i, lineID := range p.activeLines {
		iters[i] = p.freeLists[lineID].Front()
	}

	for len(res) < count {
		anyAdded := false
		for i := range iters {
			if len(res) >= count {
				break
			}
			if iters[i] != nil {
				entry := iters[i].Value.(*ipEntry)
				nodeIDs := make([]string, 0, len(entry.tunnels))
				for nodeID := range entry.tunnels {
					nodeIDs = append(nodeIDs, nodeID)
				}
				res = append(res, FreeIPInfo{
					IP:      entry.ip,
					NodeIDs: nodeIDs,
				})
				iters[i] = iters[i].Next()
				anyAdded = true
			}
		}
		if !anyAdded {
			break
		}
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
