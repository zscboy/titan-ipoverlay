package main

import (
	"fmt"
	"sync"
	"sync/atomic"
)

type PopData struct {
	IPs     []string
	rrIndex uint64
	Ref     string // Reference to base POP ID if this is an alias
}

type LoadBalancer struct {
	pops      map[string]*PopData
	relations map[string][]string // popID -> who it follows
	reverse   map[string][]string // popID -> who follows it
	mu        sync.RWMutex
}

func NewLoadBalancer(pops []PopConfig) (*LoadBalancer, error) {
	lb := &LoadBalancer{
		pops:      make(map[string]*PopData),
		relations: make(map[string][]string),
		reverse:   make(map[string][]string),
	}
	for _, p := range pops {
		var expandedIPs []string
		for ip, weight := range p.IPs {
			if weight <= 0 {
				weight = 1
			}
			for w := 0; w < weight; w++ {
				expandedIPs = append(expandedIPs, ip)
			}
		}
		lb.pops[p.ID] = &PopData{
			IPs: expandedIPs,
			Ref: p.Ref,
		}
		if len(p.Follow) > 0 {
			lb.relations[p.ID] = p.Follow
			for _, followID := range p.Follow {
				lb.reverse[followID] = append(lb.reverse[followID], p.ID)
			}
		}
	}

	// Verify all Ref pointers exist in the configuration
	for popID, data := range lb.pops {
		if data.Ref != "" {
			if _, exists := lb.pops[data.Ref]; !exists {
				return nil, fmt.Errorf("referenced POP ID %q not found for POP %q", data.Ref, popID)
			}
		}
	}

	// Initial population for followers
	for popID, follows := range lb.relations {
		lb.recalculateFollower(popID, follows)
	}

	return lb, nil
}

// GetAllUniqueIPs returns a slice of all unique configured IPs across all POPs.
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

// BalanceBySession selects an IP for a POP using round-robin.
// Stickiness is handled via external cache in the handler.
func (lb *LoadBalancer) BalanceBySession(popID string, session string, isOffline func(string) bool) (string, uint64) {
	return lb.BalanceByRR(popID, isOffline)
}

// BalanceByRR selects an IP for a POP using round-robin. Resolves reference to the first level if present.
func (lb *LoadBalancer) BalanceByRR(popID string, isOffline func(string) bool) (string, uint64) {
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

	for i := uint64(0); i < n; i++ {
		index := atomic.AddUint64(&data.rrIndex, 1) - 1
		currIndex := index % n
		ip := ipsData.IPs[currIndex]
		if !isOffline(ip) {
			return ip, index
		}
	}

	return "", 0
}

// HasPop checks if a POP exists and has IPs without advancing the counter. Resolves reference to the first level if present.
func (lb *LoadBalancer) HasPop(popID string) bool {
	lb.mu.RLock()
	defer lb.mu.RUnlock()

	data, ok := lb.pops[popID]
	if ok && data.Ref != "" {
		if nextData, nextOk := lb.pops[data.Ref]; nextOk {
			data = nextData
		}
	}

	return ok && data != nil && len(data.IPs) > 0
}

// UpdatePopIPs allows dynamic updates of the IP pool for a specific POP.
func (lb *LoadBalancer) UpdatePopIPs(popID string, ips map[string]int) {
	lb.mu.Lock()
	defer lb.mu.Unlock()

	var expandedIPs []string
	for ip, weight := range ips {
		if weight <= 0 {
			weight = 1
		}
		for w := 0; w < weight; w++ {
			expandedIPs = append(expandedIPs, ip)
		}
	}

	// 1. Update the POP itself
	if data, ok := lb.pops[popID]; ok {
		data.IPs = expandedIPs
	} else {
		lb.pops[popID] = &PopData{IPs: expandedIPs}
	}

	// 2. Propagate to all followers
	if followers, ok := lb.reverse[popID]; ok {
		for _, followerID := range followers {
			if follows, ok := lb.relations[followerID]; ok {
				lb.recalculateFollower(followerID, follows)
			}
		}
	}
}

// UpdatePopFollows allows dynamic updates of the follows relationship.
func (lb *LoadBalancer) UpdatePopFollows(popID string, follows []string) {
	lb.mu.Lock()
	defer lb.mu.Unlock()

	// 1. Clean up old reverse mapping
	if oldFollows, ok := lb.relations[popID]; ok {
		for _, oldTarget := range oldFollows {
			if targets, ok := lb.reverse[oldTarget]; ok {
				newTargets := make([]string, 0)
				for _, t := range targets {
					if t != popID {
						newTargets = append(newTargets, t)
					}
				}
				lb.reverse[oldTarget] = newTargets
			}
		}
	}

	// 2. Update relations
	lb.relations[popID] = follows

	// 3. Update new reverse mapping
	for _, targetID := range follows {
		lb.reverse[targetID] = append(lb.reverse[targetID], popID)
	}

	// 4. Recalculate
	lb.recalculateFollower(popID, follows)
}

func (lb *LoadBalancer) recalculateFollower(followerID string, follows []string) {
	var combinedIPs []string
	for _, parentID := range follows {
		if data, ok := lb.pops[parentID]; ok {
			combinedIPs = append(combinedIPs, data.IPs...)
		}
	}

	if data, ok := lb.pops[followerID]; ok {
		data.IPs = combinedIPs
	} else {
		lb.pops[followerID] = &PopData{IPs: combinedIPs}
	}
}
