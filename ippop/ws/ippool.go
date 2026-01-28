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
}

// IPPool manages a pool of unique exit IPs from connected nodes
type IPPool struct {
	mu sync.Mutex

	allIPs   map[string]*ipEntry // ip -> entry
	freeList *list.List          // List of *ipEntry (available IPs)
}

func NewIPPool() *IPPool {
	return &IPPool{
		allIPs:   make(map[string]*ipEntry),
		freeList: list.New(),
	}
}

// AddTunnel adds a tunnel to the pool. If its IP is new, it becomes available.
func (p *IPPool) AddTunnel(t *Tunnel) {
	p.mu.Lock()
	defer p.mu.Unlock()

	ip := t.opts.IP
	nodeID := t.opts.Id

	entry, ok := p.allIPs[ip]
	if !ok {
		entry = &ipEntry{
			ip:      ip,
			tunnels: make(map[string]*Tunnel),
		}
		p.allIPs[ip] = entry
		// New IP starts as free
		entry.element = p.freeList.PushBack(entry)
	}

	entry.tunnels[nodeID] = t
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

	// If the tunnel being removed was the one assigned to a session
	if entry.assignedNodeID == t.opts.Id {
		entry.assignedNodeID = ""
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
	delete(p.allIPs, ip)
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
		return entry.ip, t
	}

	return "", nil // Should not happen if Add/Remove is correct
}

// ReleaseIP returns an IP to the free list if it still has active nodes.
func (p *IPPool) ReleaseIP(ip string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	entry, ok := p.allIPs[ip]
	if !ok {
		return
	}

	// return to free list
	if entry.element == nil && len(entry.tunnels) > 0 {
		entry.assignedNodeID = ""
		entry.element = p.freeList.PushBack(entry)
	}
}

func (p *IPPool) GetIPCount() (ipCount int, freeCount int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	ipCount = len(p.allIPs)
	freeCount = p.freeList.Len()
	return
}
