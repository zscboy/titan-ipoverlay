package main

import (
	"sync"
	"sync/atomic"
)

type PopData struct {
	IPs     []string
	rrIndex uint64
}

type LoadBalancer struct {
	pops      map[string]*PopData
	relations map[string][]string // popID -> who it follows
	reverse   map[string][]string // popID -> who follows it
	mu        sync.RWMutex
}

func NewLoadBalancer(pops []PopConfig) *LoadBalancer {
	lb := &LoadBalancer{
		pops:      make(map[string]*PopData),
		relations: make(map[string][]string),
		reverse:   make(map[string][]string),
	}
	for _, p := range pops {
		lb.pops[p.ID] = &PopData{
			IPs: p.IPs,
		}
		if len(p.Follow) > 0 {
			lb.relations[p.ID] = p.Follow
			for _, followID := range p.Follow {
				lb.reverse[followID] = append(lb.reverse[followID], p.ID)
			}
		}
	}

	// Initial population for followers
	for popID, follows := range lb.relations {
		lb.recalculateFollower(popID, follows)
	}

	return lb
}

// BalanceBySession selects an IP for a POP using round-robin.
// Stickiness is handled via external cache in the handler.
func (lb *LoadBalancer) BalanceBySession(popID string, session string) string {
	return lb.BalanceByRR(popID)
}

// BalanceByRR selects an IP for a POP using round-robin.
func (lb *LoadBalancer) BalanceByRR(popID string) string {
	lb.mu.RLock()
	data, ok := lb.pops[popID]
	lb.mu.RUnlock()
	if !ok || len(data.IPs) == 0 {
		return ""
	}

	index := atomic.AddUint64(&data.rrIndex, 1) - 1
	return data.IPs[index%uint64(len(data.IPs))]
}

// HasPop checks if a POP exists and has IPs without advancing the counter.
func (lb *LoadBalancer) HasPop(popID string) bool {
	lb.mu.RLock()
	defer lb.mu.RUnlock()
	data, ok := lb.pops[popID]
	return ok && len(data.IPs) > 0
}

// UpdatePopIPs allows dynamic updates of the IP pool for a specific POP.
func (lb *LoadBalancer) UpdatePopIPs(popID string, ips []string) {
	lb.mu.Lock()
	defer lb.mu.Unlock()

	// 1. Update the parent (base POP)
	if data, ok := lb.pops[popID]; ok {
		data.IPs = ips
	} else {
		lb.pops[popID] = &PopData{IPs: ips}
	}

	// 2. Propoagate to all followers
	if followers, ok := lb.reverse[popID]; ok {
		for _, followerID := range followers {
			if follows, ok := lb.relations[followerID]; ok {
				lb.recalculateFollower(followerID, follows)
			}
		}
	}
}

// recalculateFollower updates a follower's IP list based on its parents.
// Assumes lock is already held by the caller for update, or during initialization.
func (lb *LoadBalancer) recalculateFollower(followerID string, follows []string) {
	var combinedIPs []string
	// seen := make(map[string]struct{})

	for _, parentID := range follows {
		if data, ok := lb.pops[parentID]; ok {
			// for _, ip := range data.IPs {
			// if _, ok := seen[ip]; !ok {
			combinedIPs = append(combinedIPs, data.IPs...)
			// seen[ip] = struct{}{}
			// }
			// }
		}
	}

	if data, ok := lb.pops[followerID]; ok {
		data.IPs = combinedIPs
	} else {
		lb.pops[followerID] = &PopData{IPs: combinedIPs}
	}
}
