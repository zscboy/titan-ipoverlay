package model

import (
	"context"
	"fmt"
	"time"

	goredis "github.com/redis/go-redis/v9"
	"github.com/zeromicro/go-zero/core/stores/redis"
)

const (
	dateFormat = "20060102"
	dataBit    = 40
	keepDays   = 30
)

var (
	// days is from 202501010000
	// days store in hight bits, will rank by days not traffic
	baseDate = time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
)

func AddUserDayTraffic(rdb *redis.Redis, user string, traffic int64) error {
	// Format current date as string, e.g. "20251103"
	dateStr := time.Now().Format(dateFormat)

	// Calculate how many days have passed since the base date
	daysSinceBase := int64(time.Since(baseDate).Hours() / 24)

	// Shift days to higher bits, leaving lower bits for traffic accumulation
	score := daysSinceBase << dataBit

	// Redis key for this user's traffic data
	key := fmt.Sprintf(redisKeyUserTraffic, user)

	// Add new date entry if not exists (NX)
	if _, err := rdb.Zaddnx(key, score, dateStr); err != nil {
		return fmt.Errorf("failed to add zset member: %w", err)
	}

	// Increment traffic for the current day
	if _, err := rdb.Zincrby(key, traffic, dateStr); err != nil {
		return fmt.Errorf("failed to increment traffic: %w", err)
	}

	// --- Added: trim old records, keep only the latest 30 days ---
	minScore := (daysSinceBase - keepDays + 1) << dataBit
	if _, err := rdb.Zremrangebyscore(key, 0, minScore); err != nil {
		return fmt.Errorf("failed to trim old records: %w", err)
	}

	return nil
}

func AddUsersDayTraffic(ctx context.Context, redis *redis.Redis, users map[string]int64) error {
	t := time.Now()
	dateStr := t.Format(dateFormat)

	days := int64(t.Sub(baseDate).Hours() / 24)
	days = days << dataBit
	minScore := (days - keepDays + 1) << dataBit

	pipe, err := redis.TxPipeline()
	if err != nil {
		return err
	}

	for user, traffic := range users {
		if traffic <= 0 {
			continue
		}

		key := fmt.Sprintf(redisKeyUserTraffic, user)

		// Add new date entry if not exists (NX)
		pipe.ZAddNX(ctx, key, goredis.Z{
			Score:  float64(days),
			Member: dateStr,
		})

		// Increment traffic for the current day
		pipe.ZIncrBy(ctx, key, float64(traffic), dateStr)

		// --- Added: trim old records, keep only the latest 30 days ---
		pipe.ZRemRangeByScore(ctx, key, "0", fmt.Sprintf("%d", minScore))

	}

	_, err = pipe.Exec(ctx)
	return err
}
