package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/zeromicro/go-zero/core/stores/redis/redistest"
)

func TestConfigLoad(t *testing.T) {
	cfg, err := LoadConfig("config.yaml")
	assert.NoError(t, err)
	assert.Equal(t, "0.0.0.0:8880", cfg.ListenAddr)
	assert.Equal(t, 86400, cfg.L1Cache.TTLSeconds)
	assert.Contains(t, cfg.Cells, "cell-1")
	assert.Equal(t, "http://46.250.236.15:41004", cfg.Cells["cell-1"].TargetURL)
}

func TestTwoLevelCache(t *testing.T) {
	r, clean := redistest.CreateRedisWithClean(t)
	defer clean()

	l1Conf := L1CacheConfig{
		Enabled:    true,
		TTLSeconds: 2, // 2s TTL for test
		Capacity:   100,
	}

	cache := NewTwoLevelCacheWithRedis(r, l1Conf)
	ctx := context.Background()

	testNodeID := uuid.NewString()

	// 1. Initially empty
	cellID, err := cache.GetCellID(ctx, testNodeID)
	assert.NoError(t, err)
	assert.Equal(t, "", cellID)

	// 2. Set mapping permanently to Redis, with 2s TTL in L1
	err = cache.SetCellID(ctx, testNodeID, "cell-1")
	assert.NoError(t, err)

	// 3. Query L1 hit
	cellID, err = cache.GetCellID(ctx, testNodeID)
	assert.NoError(t, err)
	assert.Equal(t, "cell-1", cellID)

	// 4. Wait 2.5s for L1 to expire
	time.Sleep(2500 * time.Millisecond)

	// 5. Query after L1 expire -> L2 Redis hit & backfill L1
	cellID, err = cache.GetCellID(ctx, testNodeID)
	assert.NoError(t, err)
	assert.Equal(t, "cell-1", cellID)
}

func TestDispatcherServerProxy(t *testing.T) {
	r, clean := redistest.CreateRedisWithClean(t)
	defer clean()

	testNodeID := uuid.NewString()

	// 1. Create a mock target Cell HTTP server
	mockCellServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Hello from Cell One! Node: " + r.URL.Query().Get("nodeid")))
	}))
	defer mockCellServer.Close()

	// 2. Create test config
	cfg := &Config{
		ListenAddr:    "127.0.0.1:0",
		L1Cache:       L1CacheConfig{Enabled: true, TTLSeconds: 10, Capacity: 100},
		DefaultCellID: "cell-1",
		Cells: map[string]CellInfo{
			"cell-1": {
				ID:        "cell-1",
				Name:      "Cell One",
				TargetURL: mockCellServer.URL,
			},
		},
	}

	cache := NewTwoLevelCacheWithRedis(r, cfg.L1Cache)
	dispatcher := NewDispatcherServer("config.yaml", cfg, cache)

	// 3. Test HTTP request to Dispatcher with valid UUID nodeid
	req := httptest.NewRequest("GET", "/node/pop?nodeid="+testNodeID, nil)
	rec := httptest.NewRecorder()

	dispatcher.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "Hello from Cell One! Node: "+testNodeID, rec.Body.String())

	// 4. Verify permanent mapping in Redis
	cellID, err := cache.GetCellID(context.Background(), testNodeID)
	assert.NoError(t, err)
	assert.Equal(t, "cell-1", cellID)
}

func TestDispatcherServerErrors(t *testing.T) {
	r, clean := redistest.CreateRedisWithClean(t)
	defer clean()

	cfg := &Config{
		ListenAddr:    "127.0.0.1:0",
		L1Cache:       L1CacheConfig{Enabled: true, TTLSeconds: 10, Capacity: 100},
		DefaultCellID: "cell-1",
		Cells: map[string]CellInfo{
			"cell-1": {
				ID:        "cell-1",
				Name:      "Cell One",
				TargetURL: "http://127.0.0.1:59999", // Unreachable target for 502 test
			},
		},
	}

	cache := NewTwoLevelCacheWithRedis(r, cfg.L1Cache)
	dispatcher := NewDispatcherServer("config.yaml", cfg, cache)

	// 1. Missing nodeid -> HTTP 400
	req400 := httptest.NewRequest("GET", "/node/pop", nil)
	rec400 := httptest.NewRecorder()
	dispatcher.ServeHTTP(rec400, req400)
	assert.Equal(t, http.StatusBadRequest, rec400.Code)
	assert.Contains(t, rec400.Body.String(), "missing nodeid")

	// 2. Unreachable target Cell -> HTTP 502 Bad Gateway
	testNodeID := uuid.NewString()
	req502 := httptest.NewRequest("GET", "/node/pop?nodeid="+testNodeID, nil)
	rec502 := httptest.NewRecorder()
	dispatcher.ServeHTTP(rec502, req502)
	assert.Equal(t, http.StatusBadGateway, rec502.Code)
	assert.Contains(t, rec502.Body.String(), "target cell gateway error")
}

func TestDispatcherServerConcurrentSameNode(t *testing.T) {
	r, clean := redistest.CreateRedisWithClean(t)
	defer clean()

	mockCellServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	}))
	defer mockCellServer.Close()

	cfg := &Config{
		ListenAddr:    "127.0.0.1:0",
		L1Cache:       L1CacheConfig{Enabled: true, TTLSeconds: 10, Capacity: 100},
		DefaultCellID: "cell-1",
		Cells: map[string]CellInfo{
			"cell-1": {ID: "cell-1", Name: "Cell One", TargetURL: mockCellServer.URL},
			"cell-2": {ID: "cell-2", Name: "Cell Two", TargetURL: mockCellServer.URL},
		},
	}

	cache := NewTwoLevelCacheWithRedis(r, cfg.L1Cache)
	dispatcher := NewDispatcherServer("config.yaml", cfg, cache)

	testNodeID := uuid.NewString()

	// Launch 50 concurrent goroutines making HTTP requests for the EXACT SAME brand new UUID nodeID
	const numConcurrent = 50
	done := make(chan bool, numConcurrent)

	for i := 0; i < numConcurrent; i++ {
		go func() {
			req := httptest.NewRequest("GET", "/node/pop?nodeid="+testNodeID, nil)
			rec := httptest.NewRecorder()
			dispatcher.ServeHTTP(rec, req)
			assert.Equal(t, http.StatusOK, rec.Code)
			done <- true
		}()
	}

	for i := 0; i < numConcurrent; i++ {
		<-done
	}

	// Verify that the node was mapped to EXACTLY ONE cell in Redis
	cellID, err := cache.GetCellID(context.Background(), testNodeID)
	assert.NoError(t, err)
	assert.Equal(t, "cell-1", cellID)
}

func TestDispatcherClientIPHeaders(t *testing.T) {
	r, clean := redistest.CreateRedisWithClean(t)
	defer clean()

	var receivedRealIP, receivedXFF string
	mockCellServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedRealIP = r.Header.Get("X-Real-IP")
		receivedXFF = r.Header.Get("X-Forwarded-For")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	}))
	defer mockCellServer.Close()

	cfg := &Config{
		ListenAddr:    "127.0.0.1:0",
		L1Cache:       L1CacheConfig{Enabled: true, TTLSeconds: 10, Capacity: 100},
		DefaultCellID: "cell-1",
		Cells: map[string]CellInfo{
			"cell-1": {ID: "cell-1", Name: "Cell One", TargetURL: mockCellServer.URL},
		},
	}

	cache := NewTwoLevelCacheWithRedis(r, cfg.L1Cache)
	dispatcher := NewDispatcherServer("config.yaml", cfg, cache)

	testNodeID := uuid.NewString()
	req := httptest.NewRequest("GET", "/node/pop?nodeid="+testNodeID, nil)
	req.RemoteAddr = "203.0.113.195:12345"
	rec := httptest.NewRecorder()

	dispatcher.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "203.0.113.195", receivedRealIP)
	assert.Contains(t, receivedXFF, "203.0.113.195")
}

func TestDispatcherSecurityFeatures(t *testing.T) {
	r, clean := redistest.CreateRedisWithClean(t)
	defer clean()

	mockCellServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	}))
	defer mockCellServer.Close()

	cfg := &Config{
		ListenAddr:    "127.0.0.1:0",
		L1Cache:       L1CacheConfig{Enabled: true, TTLSeconds: 10, Capacity: 100},
		Security:      SecurityConfig{RateLimitPerIPSec: 2},
		DefaultCellID: "cell-1",
		Cells: map[string]CellInfo{
			"cell-1": {ID: "cell-1", Name: "Cell One", TargetURL: mockCellServer.URL},
		},
	}

	cache := NewTwoLevelCacheWithRedis(r, cfg.L1Cache)
	dispatcher := NewDispatcherServer("config.yaml", cfg, cache)

	// 1. Test Non-UUID NodeID -> HTTP 400
	reqNonUUID := httptest.NewRequest("GET", "/node/pop?nodeid=not-a-valid-uuid-string", nil)
	recNonUUID := httptest.NewRecorder()
	dispatcher.ServeHTTP(recNonUUID, reqNonUUID)
	assert.Equal(t, http.StatusBadRequest, recNonUUID.Code)
	assert.Contains(t, recNonUUID.Body.String(), "nodeid must be a valid UUID")

	// 2. Test IP Rate Limiter (limit = 2 req/sec, burst = 4)
	// Make 10 requests from the same IP instantly using valid UUID
	testNodeID := uuid.NewString()
	var lastStatusCode int
	for i := 0; i < 10; i++ {
		req := httptest.NewRequest("GET", "/node/pop?nodeid="+testNodeID, nil)
		req.RemoteAddr = "198.51.100.50:54321"
		rec := httptest.NewRecorder()
		dispatcher.ServeHTTP(rec, req)
		lastStatusCode = rec.Code
	}
	// The 10th request should trigger HTTP 429 Too Many Requests
	assert.Equal(t, http.StatusTooManyRequests, lastStatusCode)
}

func TestDispatcherConfigReload(t *testing.T) {
	r, clean := redistest.CreateRedisWithClean(t)
	defer clean()

	cfg, err := LoadConfig("config.yaml")
	assert.NoError(t, err)

	cache := NewTwoLevelCacheWithRedis(r, cfg.L1Cache)
	dispatcher := NewDispatcherServer("config.yaml", cfg, cache)

	// 1. External IP (e.g. 192.168.1.100) -> HTTP 403 Forbidden
	reqExt := httptest.NewRequest("GET", "/config/reload", nil)
	reqExt.RemoteAddr = "192.168.1.100:12345"
	recExt := httptest.NewRecorder()
	dispatcher.ServeHTTP(recExt, reqExt)
	assert.Equal(t, http.StatusForbidden, recExt.Code)
	assert.Contains(t, recExt.Body.String(), "access denied")

	// 2. Localhost IP (127.0.0.1) -> HTTP 200 OK
	reqLocal := httptest.NewRequest("GET", "/config/reload", nil)
	reqLocal.RemoteAddr = "127.0.0.1:54321"
	recLocal := httptest.NewRecorder()
	dispatcher.ServeHTTP(recLocal, reqLocal)
	assert.Equal(t, http.StatusOK, recLocal.Code)
	assert.Contains(t, recLocal.Body.String(), "configuration reloaded successfully")
}
