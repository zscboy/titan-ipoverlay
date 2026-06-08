package main

import (
	"sync"
	"testing"
)

func TestBalanceByRR_BlacklistTraversal(t *testing.T) {
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

	// 2. Setup mock blacklist
	blacklist := make(map[string]bool)
	var mu sync.RWMutex
	isBlacklisted := func(ip string) bool {
		mu.RLock()
		defer mu.RUnlock()
		return blacklist[ip]
	}

	addBlacklist := func(ip string) {
		mu.Lock()
		blacklist[ip] = true
		mu.Unlock()
	}

	removeBlacklist := func(ip string) {
		mu.Lock()
		delete(blacklist, ip)
		mu.Unlock()
	}

	// Case 1: No blacklisted IPs. Should round-robin normally.
	expectedSequence := []string{"192.168.1.1", "192.168.1.2", "192.168.1.3", "192.168.1.1"}
	for i, expected := range expectedSequence {
		ip, _ := lb.BalanceByRR("pop1", isBlacklisted)
		if ip != expected {
			t.Errorf("Step %d: expected %s, got %s", i, expected, ip)
		}
	}

	// Case 2: Blacklist "192.168.1.2". Should skip it and advance rrIndex.
	addBlacklist("192.168.1.2")
	// Since last check returned 192.168.1.1 (rrIndex=4), next check starts at index 4 % 3 = 1 (which is 192.168.1.2).
	// Since 192.168.1.2 is blacklisted, it should skip it (advancing rrIndex to 5), check index 5 % 3 = 2 (192.168.1.3),
	// which is healthy and returns 192.168.1.3.
	ip, idx := lb.BalanceByRR("pop1", isBlacklisted)
	if ip != "192.168.1.3" {
		t.Errorf("Expected 192.168.1.3 after skip, got %s (idx: %d)", ip, idx)
	}

	// Next request starts at index 6 % 3 = 0 (192.168.1.1). Returns 192.168.1.1.
	ip, _ = lb.BalanceByRR("pop1", isBlacklisted)
	if ip != "192.168.1.1" {
		t.Errorf("Expected 192.168.1.1, got %s", ip)
	}

	// Next request starts at index 7 % 3 = 1 (192.168.1.2 - blacklisted).
	// Should skip (advancing to 8), check 8 % 3 = 2 (192.168.1.3). Returns 192.168.1.3.
	ip, _ = lb.BalanceByRR("pop1", isBlacklisted)
	if ip != "192.168.1.3" {
		t.Errorf("Expected 192.168.1.3, got %s", ip)
	}

	// Case 3: Recover 192.168.1.2. Should be returned again.
	removeBlacklist("192.168.1.2")
	// Next index is 9 % 3 = 0 (192.168.1.1).
	ip, _ = lb.BalanceByRR("pop1", isBlacklisted)
	if ip != "192.168.1.1" {
		t.Errorf("Expected 192.168.1.1, got %s", ip)
	}
	// Next index is 10 % 3 = 1 (192.168.1.2).
	ip, _ = lb.BalanceByRR("pop1", isBlacklisted)
	if ip != "192.168.1.2" {
		t.Errorf("Expected 192.168.1.2, got %s", ip)
	}

	// Case 4: Blacklist all IPs. Should return empty.
	addBlacklist("192.168.1.1")
	addBlacklist("192.168.1.2")
	addBlacklist("192.168.1.3")
	ip, _ = lb.BalanceByRR("pop1", isBlacklisted)
	if ip != "" {
		t.Errorf("Expected empty string when all IPs blacklisted, got %s", ip)
	}
}
