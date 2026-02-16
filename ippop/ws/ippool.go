package ws

import (
	"container/list"
	"sync"
)

// ipEntry tracks an IP and its associated tunnels
type ipEntry struct {
	ip             string
	tunnels        map[string]*Tunnel // nodeID -> Tunnel
	element        *list.Element      // Pointer to position in freeList
	assignedNodeID string             // The nodeID currently given out by AcquireIP
	isBlacklisted  bool               // New: tracks if this IP is in blacklist
}

// IPPool manages a pool of unique exit IPs from connected nodes
type IPPool struct {
	mu sync.Mutex

	allIPs         map[string]*ipEntry // ip -> entry
	freeList       *list.List          // List of *ipEntry (available IPs)
	blacklistCount int                 // New: count of IPs currently blacklisted in the pool
	assignedCount  int                 // New: count of IPs currently assigned
	tunnelCount    int                 // New: total count of tunnels in the pool
}

type PoolStats struct {
	TotalIPCount     int
	FreeIPCount      int
	BlacklistIPCount int
	AssignedIPCount  int
	TunnelCount      int
}

func NewIPPool() *IPPool {
	return &IPPool{
		allIPs:   make(map[string]*ipEntry),
		freeList: list.New(),
	}
}

// AddTunnel adds a tunnel to the pool.
func (p *IPPool) AddTunnel(t *Tunnel, isBlacklisted bool) {
	p.mu.Lock()
	defer p.mu.Unlock()

	ip := t.opts.IP
	nodeID := t.opts.Id

	entry, ok := p.allIPs[ip]
	if !ok {
		entry = &ipEntry{
			ip:            ip,
			tunnels:       make(map[string]*Tunnel),
			isBlacklisted: isBlacklisted,
		}
		p.allIPs[ip] = entry
		if isBlacklisted {
			p.blacklistCount++
		}

		// New IP starts as free if not blacklisted
		if !isBlacklisted {
			entry.element = p.freeList.PushBack(entry)
		}
	} else {
		// If entry already exists, update blacklist status if it changed
		// (e.g., node reconnected and we have new info, though usually managed by Activate/Deactivate)
		if !entry.isBlacklisted && isBlacklisted {
			entry.isBlacklisted = true
			p.blacklistCount++
			if entry.element != nil {
				p.freeList.Remove(entry.element)
				entry.element = nil
			}
		} else if entry.isBlacklisted && !isBlacklisted {
			entry.isBlacklisted = false
			p.blacklistCount--
			if entry.element == nil && entry.assignedNodeID == "" {
				entry.element = p.freeList.PushBack(entry)
			}
		}
	}

	entry.tunnels[nodeID] = t
	p.tunnelCount++
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

	// If the tunnel being removed was the one assigned to a session
	if entry.assignedNodeID == t.opts.Id {
		entry.assignedNodeID = ""
		p.assignedCount--
		// If the IP was Busy (element == nil) but still has other tunnels,
		// return it to freeList so it can be re-acquired (avoid leak)
		if entry.element == nil && len(entry.tunnels) > 0 {
			entry.element = p.freeList.PushBack(entry)
		}
	}

	// If no more tunnels for this IP, remove the IP from the pool
	if len(entry.tunnels) == 0 {
		if entry.element != nil {
			p.freeList.Remove(entry.element)
		}
		if entry.isBlacklisted {
			p.blacklistCount--
		}
		delete(p.allIPs, t.opts.IP)
	}
}

// RemoveIP removes the entire IP entry from the pool, regardless of tunnels.
func (p *IPPool) RemoveIP(ip string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	entry, ok := p.allIPs[ip]
	if !ok {
		return
	}

	if entry.element != nil {
		p.freeList.Remove(entry.element)
	}
	if entry.isBlacklisted {
		p.blacklistCount--
	}
	if entry.assignedNodeID != "" {
		p.assignedCount--
	}
	delete(p.allIPs, ip)
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

	// If it has tunnels and is not assigned, it should be in freeList
	if len(entry.tunnels) > 0 && entry.element == nil && entry.assignedNodeID == "" {
		entry.element = p.freeList.PushBack(entry)
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

	if entry.element != nil {
		p.freeList.Remove(entry.element)
		entry.element = nil
	}
}

// AcquireIP picks a free IP and one of its nodes.
// It marks the IP as busy so no other session can take it.
func (p *IPPool) AcquireIP() (string, *Tunnel) {
	p.mu.Lock()
	defer p.mu.Unlock()

	element := p.freeList.Front()
	if element == nil {
		return "", nil
	}

	entry := element.Value.(*ipEntry)
	p.freeList.Remove(element)
	entry.element = nil

	// Pick a tunnel and record its ID
	for id, t := range entry.tunnels {
		entry.assignedNodeID = id
		p.assignedCount++
		return entry.ip, t
	}

	return "", nil // Should not happen if Add/Remove is correct
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

	// return to free list only if not blacklisted and has active tunnels
	if entry.element == nil && len(entry.tunnels) > 0 && !entry.isBlacklisted {
		entry.element = p.freeList.PushBack(entry)
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
	return PoolStats{
		TotalIPCount:     len(p.allIPs),
		FreeIPCount:      p.freeList.Len(),
		BlacklistIPCount: p.blacklistCount,
		AssignedIPCount:  p.assignedCount,
		TunnelCount:      p.tunnelCount,
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
