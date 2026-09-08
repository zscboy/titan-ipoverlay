package svc

import (
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/zeromicro/go-zero/core/logx"
)

const (
	// PopStatsWindowDurationSec defines the sliding window statistics log interval in seconds.
	PopStatsWindowDurationSec = 30

	// TopPOPsLogLimit defines the max number of top POPs to output in logs after sorting.
	TopPOPsLogLimit = 10
)

type PopAccessCounter struct {
	TotalAccess int64
	UniqueCount int64    // O(1) Atomic UV Counter (eliminates Range iteration during logging)
	UniqueNodes sync.Map // map[nodeID]struct{}
}

type PopStatsCollector struct {
	lastLogTime int64
	counts      sync.Map // map[popID]*PopAccessCounter
}

type popItemStats struct {
	popID       string
	totalAccess int64
	uniqueNodes int64
}

func NewPopStatsCollector() *PopStatsCollector {
	return &PopStatsCollector{
		lastLogTime: time.Now().Unix(),
	}
}

func (c *PopStatsCollector) RecordAccess(popID, nodeID string) {
	if popID == "" {
		return
	}

	val, _ := c.counts.LoadOrStore(popID, &PopAccessCounter{})
	counter := val.(*PopAccessCounter)

	atomic.AddInt64(&counter.TotalAccess, 1)
	if nodeID != "" {
		_, loaded := counter.UniqueNodes.LoadOrStore(nodeID, struct{}{})
		if !loaded {
			// Brand new node in this window -> atomically increment UniqueCount
			atomic.AddInt64(&counter.UniqueCount, 1)
		}
	}

	// Passive Event Trigger: If PopStatsWindowDurationSec has elapsed since lastLogTime, trigger log flush
	now := time.Now().Unix()
	last := atomic.LoadInt64(&c.lastLogTime)
	if now-last >= PopStatsWindowDurationSec {
		// Atomic CAS to ensure only ONE goroutine triggers flush per window
		if atomic.CompareAndSwapInt64(&c.lastLogTime, last, now) {
			go c.flushAndLogStats()
		}
	}
}

func (c *PopStatsCollector) flushAndLogStats() {
	var items []popItemStats

	// Collect and delete keys from sync.Map to reset for next interval
	c.counts.Range(func(key, val any) bool {
		popID := key.(string)
		counter := val.(*PopAccessCounter)

		c.counts.Delete(key)

		total := atomic.LoadInt64(&counter.TotalAccess)
		uv := atomic.LoadInt64(&counter.UniqueCount) // O(1) direct atomic load

		items = append(items, popItemStats{
			popID:       popID,
			totalAccess: total,
			uniqueNodes: uv,
		})

		return true
	})

	if len(items) == 0 {
		return
	}

	// Sort POPs in descending order by TotalAccess (secondary sort by UniqueNodes)
	sort.Slice(items, func(i, j int) bool {
		if items[i].totalAccess == items[j].totalAccess {
			return items[i].uniqueNodes > items[j].uniqueNodes
		}
		return items[i].totalAccess > items[j].totalAccess
	})

	// Limit output to TopPOPsLogLimit
	if len(items) > TopPOPsLogLimit {
		items = items[:TopPOPsLogLimit]
	}

	for i, item := range items {
		logx.Infof("[POP %ds Window Stats] Rank #%d | POP: %s | Total Requests: %d | Unique Nodes: %d",
			PopStatsWindowDurationSec, i+1, item.popID, item.totalAccess, item.uniqueNodes)
	}
}
