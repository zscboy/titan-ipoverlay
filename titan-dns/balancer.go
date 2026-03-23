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
	pops map[string]*PopData
	mu   sync.RWMutex
}

func NewLoadBalancer(pops []PopConfig) *LoadBalancer {
	lb := &LoadBalancer{
		pops: make(map[string]*PopData),
	}
	for _, p := range pops {
		lb.pops[p.ID] = &PopData{
			IPs: p.IPs,
		}
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

// UpdatePopIPs allows dynamic updates of the IP pool for a specific POP.
func (lb *LoadBalancer) UpdatePopIPs(popID string, ips []string) {
	lb.mu.Lock()
	defer lb.mu.Unlock()
	if data, ok := lb.pops[popID]; ok {
		data.IPs = ips
	} else {
		lb.pops[popID] = &PopData{IPs: ips}
	}
}
