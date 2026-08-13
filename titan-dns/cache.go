package main

import (
	"sync"
	"time"
)

type CacheItem struct {
	IP        string
	CreatedAt time.Time
}

const (
	cleanupInterval  = 10 * time.Minute
	cleanupBatchSize = 5000
)

type StickyCache struct {
	items sync.Map // stores key (string) -> CacheItem
	ttl   time.Duration
}

func NewStickyCache(ttlSeconds int) *StickyCache {
	sc := &StickyCache{
		ttl: time.Duration(ttlSeconds) * time.Second,
	}
	go sc.cleanupTask()
	return sc
}

func (c *StickyCache) Get(key string) (string, bool) {
	for {
		val, ok := c.items.Load(key)
		if !ok {
			return "", false
		}

		item := val.(CacheItem)
		if time.Since(item.CreatedAt) > c.ttl {
			return "", false
		}

		// Slide expiration time while preserving the current IP
		newItem := CacheItem{
			IP:        item.IP,
			CreatedAt: time.Now(),
		}
		if c.items.CompareAndSwap(key, val, newItem) {
			return item.IP, true
		}
		// If CAS fails (due to concurrent Set or other Get), retry the load-update cycle
	}
}

func (c *StickyCache) Set(key, ip string) {
	c.items.Store(key, CacheItem{
		IP:        ip,
		CreatedAt: time.Now(),
	})
}

// RemoveByPop clears all cache entries belonging to a specific POP node.
func (c *StickyCache) RemoveByPop(popID string) {
	importSuffix := "." + popID
	c.items.Range(func(keyGen, valGen any) bool {
		k := keyGen.(string)
		// If key is exactly the popID or ends with .popID, remove it
		if k == popID || (len(k) > len(importSuffix) && k[len(k)-len(importSuffix):] == importSuffix) {
			c.items.Delete(k)
		}
		return true
	})
}

func (c *StickyCache) cleanupTask() {
	ticker := time.NewTicker(cleanupInterval)
	for range ticker.C {
		counter := 0
		c.items.Range(func(keyGen, valGen any) bool {
			k := keyGen.(string)
			v := valGen.(CacheItem)
			if time.Since(v.CreatedAt) > c.ttl {
				c.items.Delete(k)
			}

			counter++
			if counter%cleanupBatchSize == 0 {
				// Yield CPU to distribute the cleanup load (1ms sleep every cleanupBatchSize items scanned)
				time.Sleep(1 * time.Millisecond)
			}
			return true
		})
	}
}
