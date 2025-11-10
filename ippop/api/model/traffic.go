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

func AddUsersDayTraffic(ctx context.Context, rdb *redis.Redis, users map[string]int64) error {
	t := time.Now()
	dateStr := t.Format(dateFormat)

	days := int64(t.Sub(baseDate).Hours() / 24)
	days = days << dataBit
	minScore := (days - keepDays + 1) << dataBit

	pipe, err := rdb.TxPipeline()
	if err != nil {
		return err
	}

	totalTraffic := int64(0)
	for user, traffic := range users {
		if traffic <= 0 {
			continue
		}

		totalTraffic += traffic

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

	// add to all
	pipe.ZAddNX(ctx, redisKeyUserTrafficAll, goredis.Z{
		Score:  float64(days),
		Member: dateStr,
	})
	pipe.ZIncrBy(ctx, redisKeyUserTrafficAll, float64(totalTraffic), dateStr)
	pipe.ZRemRangeByScore(ctx, redisKeyUserTrafficAll, "0", fmt.Sprintf("%d", minScore))

	_, err = pipe.Exec(ctx)
	return err
}

func ListUserTraffics(ctx context.Context, rdb *redis.Redis, userName string, day int) (map[string]int64, error) {
	// Calculate how many days have passed since the base date
	daysSinceBase := int64(time.Since(baseDate).Hours() / 24)
	// Shift days to higher bits, leaving lower bits for traffic accumulation
	start := (daysSinceBase - int64(day)) << dataBit
	stop := (daysSinceBase + 1) << dataBit

	// Redis key for this user's traffic data
	key := fmt.Sprintf(redisKeyUserTraffic, userName)
	pairs, err := rdb.ZrangebyscoreWithScores(key, start, stop)
	if err != nil {
		return nil, err
	}

	userTraffics := make(map[string]int64)
	for _, pair := range pairs {
		userTraffics[pair.Key] = pair.Score & (1<<dataBit - 1)
	}
	return userTraffics, nil
}

func ListAllTraffics(ctx context.Context, rdb *redis.Redis, day int) (map[string]int64, error) {
	// Calculate how many days have passed since the base date
	daysSinceBase := int64(time.Since(baseDate).Hours() / 24)
	// Shift days to higher bits, leaving lower bits for traffic accumulation
	start := (daysSinceBase - int64(day)) << dataBit
	stop := (daysSinceBase + 1) << dataBit

	// Redis key for this user's traffic data
	pairs, err := rdb.ZrangebyscoreWithScores(redisKeyUserTrafficAll, start, stop)
	if err != nil {
		return nil, err
	}

	userTraffics := make(map[string]int64)
	for _, pair := range pairs {
		userTraffics[pair.Key] = pair.Score & (1<<dataBit - 1)
	}
	return userTraffics, nil
}
