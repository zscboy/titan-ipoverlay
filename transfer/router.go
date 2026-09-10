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
	pools       map[string]*backendPool
	cache       map[sessionKey]*SessionEntry
	timeout     time.Duration
	stopJanitor chan struct{}
}

type sessionKey struct {
	User     string
	Session  string
	Protocol string
}

type backendPool struct {
	backends []BackendInfo
	rrIndex  uint64
}

func NewRouter(backends []BackendInfo, timeout, cleanupInterval time.Duration) *Router {
	if cleanupInterval <= 0 {
		cleanupInterval = defaultRouterCleanupInterval
	}
	r := &Router{
		pools:       map[string]*backendPool{"": {backends: backends}},
		cache:       make(map[sessionKey]*SessionEntry),
		timeout:     timeout,
		stopJanitor: make(chan struct{}),
	}
	for _, backend := range backends {
		pool := r.pools[backend.Type]
		if pool == nil {
			pool = &backendPool{}
			r.pools[backend.Type] = pool
		}
		pool.backends = append(pool.backends, backend)
	}
	go r.startJanitor(cleanupInterval)
	return r
}

func (r *Router) Close() {
	close(r.stopJanitor)
}

func (r *Router) SelectBackend(username, session string) (BackendInfo, error) {
	return r.SelectBackendForProtocol(username, session, "")
}

func (r *Router) HasBackendProtocol(protocol string) bool {
	pool := r.pools[protocol]
	return pool != nil && len(pool.backends) > 0
}

func (r *Router) SelectBackendForProtocol(username, session, protocol string) (BackendInfo, error) {
	pool := r.pools[protocol]
	if pool == nil || len(pool.backends) == 0 {
		return BackendInfo{}, fmt.Errorf("no backends configured for protocol %q", protocol)
	}

	// Case 1: No session -> direct Round-Robin
	if session == "" {
		idx := atomic.AddUint64(&pool.rrIndex, 1) - 1
		backend := pool.backends[idx%uint64(len(pool.backends))]
		logx.Debugf("Router: SelectBackend - empty session, round-robin selected: %s://%s", backend.Type, backend.Addr)
		return backend, nil
	}

	// Extract real username to associate session with the specific user
	realUser := username
	parts := strings.Split(username, "-")
	if len(parts) > 0 {
		realUser = parts[0]
	}
	cacheKey := sessionKey{User: realUser, Session: session, Protocol: protocol}

	// Case 2: Session exists -> check Cache (Read Lock only)
	r.mu.RLock()
	entry, ok := r.cache[cacheKey]
	if ok {
		// Update last active time atomically without upgrading to Write Lock
		atomic.StoreInt64(&entry.LastActive, time.Now().Unix())
		backend := entry.Backend
		r.mu.RUnlock()
		logx.Debugf("Router: SelectBackend - cache hit for user session %v: %s://%s", cacheKey, backend.Type, backend.Addr)
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
	idx := atomic.AddUint64(&pool.rrIndex, 1) - 1
	backend := pool.backends[idx%uint64(len(pool.backends))]

	r.cache[cacheKey] = &SessionEntry{
		Backend:    backend,
		LastActive: time.Now().Unix(),
	}

	logx.Infof("Router: SelectBackend - cache miss for user session %v, assigned and cached: %s://%s", cacheKey, backend.Type, backend.Addr)
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
			logx.Infof("Router: Session %v expired (idle for %ds), releasing mapping to %s://%s", session, nowSec-lastActive, entry.Backend.Type, entry.Backend.Addr)
			delete(r.cache, session)
		}
	}
}
