package main

import (
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/zeromicro/go-zero/core/logx"
)

type SessionEntry struct {
	Backend    BackendInfo
	LastActive int64 // unix timestamp in seconds, updated atomically
}

type Router struct {
	mu          sync.RWMutex
	backends    []BackendInfo
	cache       map[string]*SessionEntry
	rrIndex     uint64
	timeout     time.Duration
	stopJanitor chan struct{}
}

func NewRouter(backends []BackendInfo, timeout time.Duration) *Router {
	r := &Router{
		backends:    backends,
		cache:       make(map[string]*SessionEntry),
		timeout:     timeout,
		stopJanitor: make(chan struct{}),
	}
	go r.startJanitor(10 * time.Second)
	return r
}

func (r *Router) Close() {
	close(r.stopJanitor)
}

func (r *Router) SelectBackend(username, session string) (BackendInfo, error) {
	if len(r.backends) == 0 {
		return BackendInfo{}, fmt.Errorf("no backends configured")
	}

	// Case 1: No session -> direct Round-Robin
	if session == "" {
		idx := atomic.AddUint64(&r.rrIndex, 1) - 1
		backend := r.backends[idx%uint64(len(r.backends))]
		logx.Debugf("Router: SelectBackend - empty session, round-robin selected: %s://%s", backend.Type, backend.Addr)
		return backend, nil
	}

	// Extract real username to associate session with the specific user
	realUser := username
	parts := strings.Split(username, "-")
	if len(parts) > 0 {
		realUser = parts[0]
	}
	cacheKey := realUser + ":" + session

	// Case 2: Session exists -> check Cache (Read Lock only)
	r.mu.RLock()
	entry, ok := r.cache[cacheKey]
	if ok {
		// Update last active time atomically without upgrading to Write Lock
		atomic.StoreInt64(&entry.LastActive, time.Now().Unix())
		backend := entry.Backend
		r.mu.RUnlock()
		logx.Debugf("Router: SelectBackend - cache hit for user session %s: %s://%s", cacheKey, backend.Type, backend.Addr)
		return backend, nil
	}
	r.mu.RUnlock()

	// Case 3: Session Cache Miss -> Write Lock
	r.mu.Lock()
	defer r.mu.Unlock()

	// Double check under write lock
	if entry, ok = r.cache[cacheKey]; ok {
		atomic.StoreInt64(&entry.LastActive, time.Now().Unix())
		return entry.Backend, nil
	}

	// Round-Robin to select backend
	idx := atomic.AddUint64(&r.rrIndex, 1) - 1
	backend := r.backends[idx%uint64(len(r.backends))]

	r.cache[cacheKey] = &SessionEntry{
		Backend:    backend,
		LastActive: time.Now().Unix(),
	}

	logx.Infof("Router: SelectBackend - cache miss for user session %s, assigned and cached: %s://%s", cacheKey, backend.Type, backend.Addr)
	return backend, nil
}

func (r *Router) startJanitor(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			r.cleanupExpired()
		case <-r.stopJanitor:
			return
		}
	}
}

func (r *Router) cleanupExpired() {
	r.mu.Lock()
	defer r.mu.Unlock()

	nowSec := time.Now().Unix()
	timeoutSec := int64(r.timeout.Seconds())

	for session, entry := range r.cache {
		lastActive := atomic.LoadInt64(&entry.LastActive)
		if nowSec-lastActive > timeoutSec {
			logx.Infof("Router: Session %s expired (idle for %ds), releasing mapping to %s://%s", session, nowSec-lastActive, entry.Backend.Type, entry.Backend.Addr)
			delete(r.cache, session)
		}
	}
}
