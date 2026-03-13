package main

import (
	"sync"
	"time"
)

type CacheItem struct {
	IP        string
	CreatedAt time.Time
}

type StickyCache struct {
	mu    sync.RWMutex
	items map[string]CacheItem
	ttl   time.Duration
}

func NewStickyCache(ttlSeconds int) *StickyCache {
	sc := &StickyCache{
		items: make(map[string]CacheItem),
		ttl:   time.Duration(ttlSeconds) * time.Second,
	}
	go sc.cleanupTask()
	return sc
}

func (c *StickyCache) Get(key string) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	item, ok := c.items[key]
	if !ok {
		return "", false
	}

	if time.Since(item.CreatedAt) > c.ttl {
		delete(c.items, key)
		return "", false
	}

	// Update CreatedAt to reset effective life time
	item.CreatedAt = time.Now()
	c.items[key] = item

	return item.IP, true
}

func (c *StickyCache) Set(key, ip string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.items[key] = CacheItem{
		IP:        ip,
		CreatedAt: time.Now(),
	}
}

func (c *StickyCache) cleanupTask() {
	ticker := time.NewTicker(1 * time.Hour)
	for range ticker.C {
		c.mu.Lock()
		for k, v := range c.items {
			if time.Since(v.CreatedAt) > c.ttl {
				delete(c.items, k)
			}
		}
		c.mu.Unlock()
	}
}
