package main

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestRouterProtocolPools(t *testing.T) {
	socks := BackendInfo{Type: "socks5", Addr: "localhost:1080"}
	a := BackendInfo{Type: "http", Addr: "localhost:8080"}
	b := BackendInfo{Type: "http", Addr: "localhost:8081"}
	router := NewRouter([]BackendInfo{socks, a, b}, time.Hour, defaultRouterCleanupInterval)
	defer router.Close()
	for _, want := range []BackendInfo{a, b, a, b} {
		got, err := router.SelectBackendForProtocol("", "", "http")
		if err != nil || got != want {
			t.Fatalf("HTTP round-robin got %+v, err=%v, want %+v", got, err, want)
		}
		// Legacy callers retain their own pool and counter.
		if _, err := router.SelectBackend("", ""); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 2; i++ {
		if _, err := router.SelectBackend("", ""); err != nil {
			t.Fatal(err)
		}
	}
	legacy, err := router.SelectBackend("user-session-one", "one")
	if err != nil {
		t.Fatal(err)
	}
	if legacy != socks {
		t.Fatalf("expected a SOCKS5 mapping to exercise protocol isolation, got %+v", legacy)
	}
	first, err := router.SelectBackendForProtocol("user-session-one", "one", "http")
	if err != nil || first != a {
		t.Fatalf("unexpected initial HTTP selection: %+v, err=%v", first, err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, err := router.SelectBackendForProtocol("user-session-one", "one", "http")
			if err != nil || got != first {
				t.Errorf("session mapping changed: %+v, err=%v", got, err)
			}
		}()
	}
	wg.Wait()
	if got, err := router.SelectBackend("user-session-one", "one"); err != nil || got != legacy {
		t.Fatalf("HTTP selection changed legacy session mapping: %+v, err=%v", got, err)
	}
	if got, err := router.SelectBackendForProtocol("other-session-one", "one", "http"); err != nil || got != b {
		t.Fatalf("users share a session mapping: %+v, err=%v", got, err)
	}
	if _, err := router.SelectBackendForProtocol("", "", "unknown"); err == nil {
		t.Fatal("missing protocol should fail without falling back to another pool")
	}
	router.mu.Lock()
	for _, entry := range router.cache {
		atomic.StoreInt64(&entry.LastActive, time.Now().Add(-2*time.Hour).Unix())
	}
	router.mu.Unlock()
	router.cleanupExpired()
	if len(router.cache) != 0 {
		t.Fatal("expired protocol session mappings were not cleaned up")
	}
}

func TestRouterUsesCleanupInterval(t *testing.T) {
	router := NewRouter([]BackendInfo{{Type: "http", Addr: "localhost:8080"}}, time.Hour, 10*time.Millisecond)
	defer router.Close()
	if _, err := router.SelectBackendForProtocol("user-session-one", "one", "http"); err != nil {
		t.Fatal(err)
	}
	router.mu.Lock()
	for _, entry := range router.cache {
		atomic.StoreInt64(&entry.LastActive, time.Now().Add(-2*time.Hour).Unix())
	}
	router.mu.Unlock()
	deadline := time.After(2 * time.Second)
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-deadline:
			t.Fatal("janitor did not remove expired entries using the configured interval")
		case <-ticker.C:
			router.mu.RLock()
			empty := len(router.cache) == 0
			router.mu.RUnlock()
			if empty {
				return
			}
		}
	}
}
