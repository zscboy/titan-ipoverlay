package main

import (
	"sync/atomic"
)

type LoadBalancer struct {
	pops     []PopConfig
	rrIndex  uint64
}

func NewLoadBalancer(pops []PopConfig) *LoadBalancer {
	return &LoadBalancer{
		pops: pops,
	}
}

// NextIP selects a POP IP using round-robin.
// In a real load-balanced scenario, this would check POP scores or connection counts.
func (lb *LoadBalancer) NextIP() string {
	if len(lb.pops) == 0 {
		return ""
	}

	index := atomic.AddUint64(&lb.rrIndex, 1) - 1
	pop := lb.pops[index % uint64(len(lb.pops))]
	return pop.IP
}

// UpdateLoad can be used to dynamically adjust weights based on back-end reports.
func (lb *LoadBalancer) UpdateLoad(popID string, load int) {
	// Not implemented in this basic version, but provides a hook for future POP load feedback.
}
