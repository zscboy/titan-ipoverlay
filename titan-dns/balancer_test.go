package main

import (
	"sync"
	"testing"
)

func TestBalanceByRR_OfflineIPsTraversal(t *testing.T) {
	// 1. Setup Pops Configuration
	pops := []PopConfig{
		{
			ID:   "pop1",
			Name: "Singapore Node",
			IPs:  []string{"192.168.1.1", "192.168.1.2", "192.168.1.3"},
		},
	}

	lb, err := NewLoadBalancer(pops)
	if err != nil {
		t.Fatalf("failed to create load balancer: %v", err)
	}

	// 2. Setup mock offline registry
	offlineIPs := make(map[string]bool)
	var mu sync.RWMutex
	isOffline := func(ip string) bool {
		mu.RLock()
		defer mu.RUnlock()
		return offlineIPs[ip]
	}

	markOffline := func(ip string) {
		mu.Lock()
		offlineIPs[ip] = true
		mu.Unlock()
	}

	markOnline := func(ip string) {
		mu.Lock()
		delete(offlineIPs, ip)
		mu.Unlock()
	}

	// Case 1: No offline IPs. Should round-robin normally.
	expectedSequence := []string{"192.168.1.1", "192.168.1.2", "192.168.1.3", "192.168.1.1"}
	for i, expected := range expectedSequence {
		ip, _ := lb.BalanceByRR("pop1", isOffline)
		if ip != expected {
			t.Errorf("Step %d: expected %s, got %s", i, expected, ip)
		}
	}

	// Case 2: Mark "192.168.1.2" offline. Should skip it and advance rrIndex.
	markOffline("192.168.1.2")
	// Since last check returned 192.168.1.1 (rrIndex=4), next check starts at index 4 % 3 = 1 (which is 192.168.1.2).
	// Since 192.168.1.2 is offline, it should skip it (advancing rrIndex to 5), check index 5 % 3 = 2 (192.168.1.3),
	// which is healthy and returns 192.168.1.3.
	ip, idx := lb.BalanceByRR("pop1", isOffline)
	if ip != "192.168.1.3" {
		t.Errorf("Expected 192.168.1.3 after skip, got %s (idx: %d)", ip, idx)
	}

	// Next request starts at index 6 % 3 = 0 (192.168.1.1). Returns 192.168.1.1.
	ip, _ = lb.BalanceByRR("pop1", isOffline)
	if ip != "192.168.1.1" {
		t.Errorf("Expected 192.168.1.1, got %s", ip)
	}

	// Next request starts at index 7 % 3 = 1 (192.168.1.2 - offline).
	// Should skip (advancing to 8), check 8 % 3 = 2 (192.168.1.3). Returns 192.168.1.3.
	ip, _ = lb.BalanceByRR("pop1", isOffline)
	if ip != "192.168.1.3" {
		t.Errorf("Expected 192.168.1.3, got %s", ip)
	}

	// Case 3: Recover 192.168.1.2. Should be returned again.
	markOnline("192.168.1.2")
	// Next index is 9 % 3 = 0 (192.168.1.1).
	ip, _ = lb.BalanceByRR("pop1", isOffline)
	if ip != "192.168.1.1" {
		t.Errorf("Expected 192.168.1.1, got %s", ip)
	}
	// Next index is 10 % 3 = 1 (192.168.1.2).
	ip, _ = lb.BalanceByRR("pop1", isOffline)
	if ip != "192.168.1.2" {
		t.Errorf("Expected 192.168.1.2, got %s", ip)
	}

	// Case 4: Mark all IPs offline. Should return empty.
	markOffline("192.168.1.1")
	markOffline("192.168.1.2")
	markOffline("192.168.1.3")
	ip, _ = lb.BalanceByRR("pop1", isOffline)
	if ip != "" {
		t.Errorf("Expected empty string when all IPs offline, got %s", ip)
	}
}
