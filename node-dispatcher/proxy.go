package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"sync"
	"sync/atomic"
	"time"

	"github.com/bluele/gcache"
	"github.com/golang/groupcache/singleflight"
	"github.com/google/uuid"
	"github.com/zeromicro/go-zero/core/logx"
	"golang.org/x/time/rate"
)

const (
	// DefaultIPRateLimiterCapacity defines the max active IP limiters in LRU cache (~5MB RAM footprint)
	DefaultIPRateLimiterCapacity = 50000

	// DefaultIPRateLimiterTTL defines idle duration after which an inactive IP limiter is evicted
	DefaultIPRateLimiterTTL = 3 * time.Minute

	// DefaultRateLimitBurstMultiplier defines the token bucket burst size relative to limitPerSec
	DefaultRateLimitBurstMultiplier = 2

	// Network transport connection pool constants
	DefaultTCPKeepAliveDuration  = 30 * time.Second
	DefaultTLSHandshakeTimeout   = 5 * time.Second
	DefaultExpectContinueTimeout = 1 * time.Second

	// HTTP Header and Query parameter keys
	HeaderXNodeID       = "X-Node-ID"
	HeaderXRealIP       = "X-Real-IP"
	HeaderXForwardedFor = "X-Forwarded-For"
	QueryParamNodeID    = "nodeid"
)

// JSONErrorResponse represents an enterprise standard error response payload.
type JSONErrorResponse struct {
	Error     string `json:"error"`
	Details   string `json:"details,omitempty"`
	NodeID    string `json:"nodeid,omitempty"`
	Timestamp int64  `json:"timestamp"`
}

func writeJSONError(w http.ResponseWriter, statusCode int, errMessage string, details string, nodeID string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(statusCode)

	resp := JSONErrorResponse{
		Error:     errMessage,
		Details:   details,
		NodeID:    nodeID,
		Timestamp: time.Now().Unix(),
	}
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		logx.Errorf("[writeJSONError Failure] failed to encode JSON response (statusCode: %d, errMessage: %s): %v", statusCode, errMessage, err)
	}
}

// IPRateLimiter manages per-IP rate limiters using a memory-bounded LRU cache with TTL expiration.
// Uses singleflight coalescing to eliminate global lock contention on concurrent new IP arrivals.
type IPRateLimiter struct {
	cache   gcache.Cache
	r       rate.Limit
	b       int
	sfGroup singleflight.Group
}

func NewIPRateLimiter(limitPerSec int) *IPRateLimiter {
	if limitPerSec <= 0 {
		return nil
	}

	// Cap at DefaultIPRateLimiterCapacity active IP limiters in RAM (~5MB), auto-expire inactive IPs after DefaultIPRateLimiterTTL
	cache := gcache.New(DefaultIPRateLimiterCapacity).
		LRU().
		Expiration(DefaultIPRateLimiterTTL).
		Build()

	return &IPRateLimiter{
		cache: cache,
		r:     rate.Limit(limitPerSec),
		b:     limitPerSec * DefaultRateLimitBurstMultiplier,
	}
}

func (i *IPRateLimiter) GetLimiter(ip string) *rate.Limiter {
	// 1. FAST PATH: Read from LRU cache directly (99.9% requests hit here, zero locks!)
	val, err := i.cache.Get(ip)
	if err == nil && val != nil {
		return val.(*rate.Limiter)
	}

	// 2. SLOW PATH: Use singleflight to coalesce concurrent new IP initialization (zero lock contention across different IPs!)
	limiterVal, _ := i.sfGroup.Do(ip, func() (interface{}, error) {
		val, err := i.cache.Get(ip)
		if err == nil && val != nil {
			return val.(*rate.Limiter), nil
		}

		limiter := rate.NewLimiter(i.r, i.b)
		if err := i.cache.Set(ip, limiter); err != nil {
			logx.Errorf("[IPRateLimiter Set Cache Error] ip: %s, err: %v", ip, err)
		}
		return limiter, nil
	})

	return limiterVal.(*rate.Limiter)
}

// DispatcherServer handles HTTP forwarding for node requests and 127.0.0.1 hot config reload.
type DispatcherServer struct {
	configPath  string
	mu          sync.RWMutex
	cfg         *Config
	cache       *TwoLevelCache
	cellProxies map[string]*httputil.ReverseProxy // Pre-created reverse proxies per cellID (zero allocations on request path)
	cellIDs     []string                           // Pre-computed cell IDs for round-robin allocation
	sfGroup     singleflight.Group                 // Singleflight group to prevent allocation race condition for same nodeID
	ipLimiter   *IPRateLimiter
	rrIndex     uint64
}

// NewDispatcherServer initializes DispatcherServer instance.
func NewDispatcherServer(configPath string, cfg *Config, cache *TwoLevelCache) *DispatcherServer {
	s := &DispatcherServer{
		configPath: configPath,
		cfg:        cfg,
		cache:      cache,
	}

	s.cellProxies, s.cellIDs, s.ipLimiter = s.buildProxiesAndLimiter(cfg)
	return s
}

// buildProxiesAndLimiter constructs ReverseProxy mappings, cell ID lists, and IP rate limiter from Config.
func (s *DispatcherServer) buildProxiesAndLimiter(cfg *Config) (map[string]*httputil.ReverseProxy, []string, *IPRateLimiter) {
	var ipLimiter *IPRateLimiter
	if cfg.Security.RateLimitPerIPSec > 0 {
		ipLimiter = NewIPRateLimiter(cfg.Security.RateLimitPerIPSec)
	}

	// Enterprise High-Performance HTTP Transport (configured via Config tags for million-QPS connection pooling)
	sharedTransport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   time.Duration(cfg.HTTPProxy.DialTimeoutSec) * time.Second,
			KeepAlive: DefaultTCPKeepAliveDuration,
		}).DialContext,
		MaxIdleConns:          cfg.HTTPProxy.MaxIdleConns,
		MaxIdleConnsPerHost:   cfg.HTTPProxy.MaxIdleConnsPerHost,
		IdleConnTimeout:       time.Duration(cfg.HTTPProxy.IdleConnTimeoutSec) * time.Second,
		TLSHandshakeTimeout:   DefaultTLSHandshakeTimeout,
		ExpectContinueTimeout: DefaultExpectContinueTimeout,
	}

	cellProxies := make(map[string]*httputil.ReverseProxy, len(cfg.Cells))
	var cellIDs []string

	for cellID, cellInfo := range cfg.Cells {
		cellIDs = append(cellIDs, cellID)

		targetURL, err := url.Parse(cellInfo.TargetURL)
		if err != nil {
			logx.Errorf("[DispatcherServer Error] Invalid target_url '%s' for cell '%s': %v", cellInfo.TargetURL, cellID, err)
			continue
		}

		proxy := httputil.NewSingleHostReverseProxy(targetURL)
		proxy.Transport = sharedTransport

		// Attach enterprise ErrorHandler for detailed dial logging and standard JSON error response
		cID := cellID
		tURL := cellInfo.TargetURL
		proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, proxyErr error) {
			reqNodeID := r.URL.Query().Get(QueryParamNodeID)
			if reqNodeID == "" {
				reqNodeID = r.Header.Get(HeaderXNodeID)
			}
			logx.WithContext(r.Context()).Errorf("[ReverseProxy Target Error] cellID: %s, nodeID: %s, target: %s, path: %s, err: %v", cID, reqNodeID, tURL, r.URL.Path, proxyErr)
			writeJSONError(w, http.StatusBadGateway, "target cell gateway error", fmt.Sprintf("failed to dial target cell '%s' at %s: %v", cID, tURL, proxyErr), reqNodeID)
		}

		cellProxies[cellID] = proxy
	}

	return cellProxies, cellIDs, ipLimiter
}

func (s *DispatcherServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Special Management Endpoint: Dynamic Config Reload API (Restricted strictly to 127.0.0.1 local requests)
	if r.URL.Path == "/config/reload" {
		s.handleConfigReload(w, r)
		return
	}

	// Lock-free RLock read of current server state
	s.mu.RLock()
	cfg := s.cfg
	cellProxies := s.cellProxies
	cellIDs := s.cellIDs
	ipLimiter := s.ipLimiter
	s.mu.RUnlock()

	// 0. Security Guard 1: IP Rate Limiting (Intercept DDoS / IP spamming)
	if ipLimiter != nil {
		clientIP, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			clientIP = r.RemoteAddr
		}
		limiter := ipLimiter.GetLimiter(clientIP)
		if !limiter.Allow() {
			logx.WithContext(ctx).Errorf("[Security RateLimit] Intercepted request from IP %s (exceeded %d req/sec)", clientIP, cfg.Security.RateLimitPerIPSec)
			writeJSONError(w, http.StatusTooManyRequests, "rate limit exceeded", fmt.Sprintf("request rate exceeded limit of %d req/sec per IP", cfg.Security.RateLimitPerIPSec), "")
			return
		}
	}

	// 1. Extract nodeID from query param "nodeid" or HTTP header "X-Node-ID"
	nodeID := r.URL.Query().Get(QueryParamNodeID)
	if nodeID == "" {
		nodeID = r.Header.Get(HeaderXNodeID)
	}

	if nodeID == "" {
		logx.WithContext(ctx).Errorf("Missing nodeid in request from %s: URL=%s", r.RemoteAddr, r.URL.String())
		writeJSONError(w, http.StatusBadRequest, "missing nodeid", "nodeid query param or X-Node-ID header is required", "")
		return
	}

	// Security Guard 2: Strict UUID Format Validation (Intercept fake/random non-UUID nodeIDs)
	if _, err := uuid.Parse(nodeID); err != nil {
		logx.WithContext(ctx).Errorf("[Security Intercept] Invalid nodeID UUID format from %s: nodeID=%s, err=%v", r.RemoteAddr, nodeID, err)
		writeJSONError(w, http.StatusBadRequest, "invalid nodeid", "nodeid must be a valid UUID", nodeID)
		return
	}

	// 2. FAST PATH: Check two-level cache directly (99.9% hit rate, zero locks, zero singleflight overhead)
	cellID, err := s.cache.GetCellID(ctx, nodeID)
	if err != nil {
		logx.WithContext(ctx).Errorf("[ServeHTTP Cache Query Error] nodeID: %s, err: %v", nodeID, err)
		writeJSONError(w, http.StatusInternalServerError, "cache query error", err.Error(), nodeID)
		return
	}

	// 3. SLOW PATH: Only use Singleflight protection for cache misses / new node allocations
	if cellID == "" {
		cellIDVal, err := s.sfGroup.Do("cell_resolve:"+nodeID, func() (interface{}, error) {
			// Double-check cache inside singleflight to avoid duplicate allocation
			cellID, err := s.cache.GetCellID(ctx, nodeID)
			if err == nil && cellID != "" {
				return cellID, nil
			}

			// Allocate a cell ID for the new node
			cellID = s.allocateCell(ctx, nodeID, cfg, cellIDs)
			if cellID == "" {
				return "", fmt.Errorf("no available cell configured to allocate for nodeID %s", nodeID)
			}
			if err := s.cache.SetCellID(ctx, nodeID, cellID); err != nil {
				logx.WithContext(ctx).Errorf("[ServeHTTP SetCellID Failure] nodeID: %s -> cellID: %s, err: %v", nodeID, cellID, err)
			}

			return cellID, nil
		})

		if err != nil {
			logx.WithContext(ctx).Errorf("[ServeHTTP Cell Resolution Error] nodeID: %s, err: %v", nodeID, err)
			writeJSONError(w, http.StatusServiceUnavailable, "cell resolution failure", err.Error(), nodeID)
			return
		}

		cellID = cellIDVal.(string)
	}

	// 4. Retrieve pre-created ReverseProxy for allocated cellID
	proxy, exists := cellProxies[cellID]
	if !exists || proxy == nil {
		logx.WithContext(ctx).Errorf("[ServeHTTP Cell Config Error] cellID '%s' for nodeID '%s' proxy not initialized", cellID, nodeID)
		writeJSONError(w, http.StatusServiceUnavailable, "cell proxy unavailable", fmt.Sprintf("proxy for cell '%s' is not available", cellID), nodeID)
		return
	}

	cellInfo := cfg.Cells[cellID]
	logx.WithContext(ctx).Infof("[Dispatching Request] nodeID: %s -> cellID: %s -> target: %s", nodeID, cellID, cellInfo.TargetURL)

	// 5. Explicitly ensure client's real IP headers (X-Real-IP & X-Forwarded-For) are forwarded
	setClientIPHeaders(r)

	// 6. Forward HTTP request to target Cell URL using zero-allocation pre-created proxy
	proxy.ServeHTTP(w, r)
}

// handleConfigReload dynamically reloads configuration file from disk at runtime.
// Restricted strictly to local requests from 127.0.0.1 / ::1 / localhost.
func (s *DispatcherServer) handleConfigReload(w http.ResponseWriter, r *http.Request) {
	clientIP, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		clientIP = r.RemoteAddr
	}

	// 1. Security Check: Allow ONLY localhost / 127.0.0.1 / ::1 callers
	if clientIP != "127.0.0.1" && clientIP != "::1" && clientIP != "localhost" {
		logx.WithContext(r.Context()).Errorf("[Security Intercept] Unauthorized config reload attempt from non-localhost IP: %s", r.RemoteAddr)
		writeJSONError(w, http.StatusForbidden, "access denied", "config reload API is restricted to 127.0.0.1 local requests only", "")
		return
	}

	// 2. Load and validate new configuration file
	newCfg, err := LoadConfig(s.configPath)
	if err != nil {
		logx.WithContext(r.Context()).Errorf("[Config Reload Failure] Failed to parse config from '%s': %v", s.configPath, err)
		writeJSONError(w, http.StatusBadRequest, "config reload failure", fmt.Sprintf("failed to parse config file '%s': %v", s.configPath, err), "")
		return
	}

	// 3. Rebuild proxies, cell IDs, and rate limiters
	newCellProxies, newCellIDs, newIPLimiter := s.buildProxiesAndLimiter(newCfg)

	// 4. Thread-safe atomic hot swap of server state
	s.mu.Lock()
	s.cfg = newCfg
	s.cellProxies = newCellProxies
	s.cellIDs = newCellIDs
	s.ipLimiter = newIPLimiter
	s.mu.Unlock()

	logx.WithContext(r.Context()).Infof("[Config Reload Success] Dynamically reloaded config from '%s'. Active cells: %d", s.configPath, len(newCfg.Cells))

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status":      "ok",
		"message":     "configuration reloaded successfully",
		"config_path": s.configPath,
		"cells_count": len(newCfg.Cells),
		"timestamp":   time.Now().Unix(),
	})
}

// allocateCell allocates a cellID for a new node (uses DefaultCellID or round-robin)
func (s *DispatcherServer) allocateCell(ctx context.Context, nodeID string, cfg *Config, cellIDs []string) string {
	if cfg.DefaultCellID != "" {
		if _, ok := cfg.Cells[cfg.DefaultCellID]; ok {
			logx.WithContext(ctx).Infof("[Allocate Default Cell] nodeID: %s -> %s", nodeID, cfg.DefaultCellID)
			return cfg.DefaultCellID
		}
	}

	if len(cellIDs) == 0 {
		return ""
	}

	idx := atomic.AddUint64(&s.rrIndex, 1) % uint64(len(cellIDs))
	allocated := cellIDs[idx]
	logx.WithContext(ctx).Infof("[Allocate Round-Robin Cell] nodeID: %s -> %s", nodeID, allocated)
	return allocated
}

// setClientIPHeaders ensures the client's real IP address is explicitly forwarded
// via X-Real-IP and X-Forwarded-For headers to downstream target servers.
func setClientIPHeaders(r *http.Request) {
	clientIP, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		clientIP = r.RemoteAddr
	}

	// Set X-Real-IP if not present
	if r.Header.Get(HeaderXRealIP) == "" {
		r.Header.Set(HeaderXRealIP, clientIP)
	}

	// Append or set X-Forwarded-For
	if xff := r.Header.Get(HeaderXForwardedFor); xff == "" {
		r.Header.Set(HeaderXForwardedFor, clientIP)
	} else {
		r.Header.Set(HeaderXForwardedFor, xff+", "+clientIP)
	}
}
