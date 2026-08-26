package main

import (
	"context"
	"fmt"
	"time"

	"github.com/bluele/gcache"
	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/stores/redis"
)

const RedisKeyNodeCellHash = "titan:node:cell"

// TwoLevelCache implements a two-level cache:
// - L1: Local in-memory LRU cache with configurable TTL
// - L2: Permanent Redis Hash storage (HGET/HSET on titan:node:cell)
type TwoLevelCache struct {
	l1Cache gcache.Cache
	l2Redis *redis.Redis
	ttl     time.Duration
}

// NewTwoLevelCache initializes Redis from RedisConf and local memory L1 cache.
func NewTwoLevelCache(redisConf redis.RedisConf, l1Conf L1CacheConfig) *TwoLevelCache {
	r := redis.MustNewRedis(redisConf)
	return NewTwoLevelCacheWithRedis(r, l1Conf)
}

// NewTwoLevelCacheWithRedis initializes with an existing redis.Redis client.
func NewTwoLevelCacheWithRedis(r *redis.Redis, l1Conf L1CacheConfig) *TwoLevelCache {
	ttl := time.Duration(l1Conf.TTLSeconds) * time.Second

	var l1 gcache.Cache
	if l1Conf.Enabled {
		l1 = gcache.New(l1Conf.Capacity).
			LRU().
			Expiration(ttl).
			Build()
	}

	return &TwoLevelCache{
		l1Cache: l1,
		l2Redis: r,
		ttl:     ttl,
	}
}

// GetCellID queries L1 memory cache first; if missed, queries L2 Redis Hash (HGET titan:node:cell nodeID).
// On L2 Redis hit, it backfills the L1 local cache with configured TTL.
func (c *TwoLevelCache) GetCellID(ctx context.Context, nodeID string) (string, error) {
	// 1. Check L1 local memory cache
	if c.l1Cache != nil {
		val, err := c.l1Cache.Get(nodeID)
		if err == nil && val != nil {
			if cellID, ok := val.(string); ok && cellID != "" {
				logx.WithContext(ctx).Debugf("[L1 Memory Hit] nodeID: %s -> cellID: %s", nodeID, cellID)
				return cellID, nil
			}
		} else if err != nil && err != gcache.KeyNotFoundError {
			logx.WithContext(ctx).Errorf("[L1 Memory Cache Get Error] nodeID: %s, err: %v", nodeID, err)
		}
	}

	// 2. Check L2 Redis Hash (permanent storage via HGET titan:node:cell nodeID)
	cellID, err := c.l2Redis.HgetCtx(ctx, RedisKeyNodeCellHash, nodeID)
	if err != nil {
		if err == redis.Nil {
			logx.WithContext(ctx).Debugf("[Cache Miss] nodeID: %s not mapped in L2 Redis Hash", nodeID)
			return "", nil
		}
		logx.WithContext(ctx).Errorf("[L2 Redis HGET Failure] nodeID: %s, key: %s, err: %v", nodeID, RedisKeyNodeCellHash, err)
		return "", fmt.Errorf("redis HGET error for nodeID %s (key %s): %w", nodeID, RedisKeyNodeCellHash, err)
	}

	if cellID != "" {
		logx.WithContext(ctx).Debugf("[L2 Redis Hit] nodeID: %s -> cellID: %s", nodeID, cellID)
		// Backfill L1 local cache with configurable TTL
		if c.l1Cache != nil {
			if err := c.l1Cache.SetWithExpire(nodeID, cellID, c.ttl); err != nil {
				logx.WithContext(ctx).Errorf("[L1 Backfill Error] nodeID: %s, cellID: %s, err: %v", nodeID, cellID, err)
			}
		}
		return cellID, nil
	}

	logx.WithContext(ctx).Debugf("[Cache Miss] nodeID: %s empty mapping in Redis Hash", nodeID)
	return "", nil
}

// SetCellID permanently saves nodeID -> cellID mapping to L2 Redis Hash (HSET titan:node:cell nodeID cellID)
// and sets L1 local memory cache with configurable TTL.
func (c *TwoLevelCache) SetCellID(ctx context.Context, nodeID, cellID string) error {
	// Save permanently to Redis Hash
	if err := c.l2Redis.HsetCtx(ctx, RedisKeyNodeCellHash, nodeID, cellID); err != nil {
		logx.WithContext(ctx).Errorf("[L2 Redis HSET Failure] nodeID: %s, cellID: %s, key: %s, err: %v", nodeID, cellID, RedisKeyNodeCellHash, err)
		return fmt.Errorf("failed to save node mapping to Redis Hash (nodeID: %s, cellID: %s): %w", nodeID, cellID, err)
	}

	// Set L1 local memory cache with TTL
	if c.l1Cache != nil {
		if err := c.l1Cache.SetWithExpire(nodeID, cellID, c.ttl); err != nil {
			logx.WithContext(ctx).Errorf("[L1 Memory Set Error] nodeID: %s, cellID: %s, err: %v", nodeID, cellID, err)
		}
	}

	logx.WithContext(ctx).Infof("[Mapping Saved] nodeID: %s -> cellID: %s (Redis Hash Permanent, L1 TTL: %v)", nodeID, cellID, c.ttl)
	return nil
}
