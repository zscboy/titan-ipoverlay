package model

import (
	"context"
	"fmt"
	"strconv"
	"time"

	goredis "github.com/redis/go-redis/v9"
	"github.com/zeromicro/go-zero/core/stores/redis"
)

const (
	fiveMinDataBit = 29 // 低 29 bit 存流量
	keep24Hours    = 24 // 保留 24小时
	fiveMinutes    = 5 * 60
	KB             = 1024
)

func floorTo5Min(t time.Time) int64 {
	return t.Unix() / fiveMinutes // fiveMinux = 5分钟
}

// 保存用户每 5 分钟流量
func AddUsersTrafficFiveMinutes(ctx context.Context, rdb *redis.Redis, users map[string]int64) error {
	now := time.Now()
	ts := floorTo5Min(now)            // 5 分钟粒度时间戳
	scoreBase := ts << fiveMinDataBit // 高位存时间戳
	minScore := (now.Add(-time.Hour*keep24Hours).Unix() / fiveMinutes) << fiveMinDataBit

	pipe, err := rdb.TxPipeline()
	if err != nil {
		return err
	}

	total := int64(0)

	for user, traffic := range users {
		if traffic <= 0 {
			continue
		}

		trafficKB := traffic / KB

		total += trafficKB
		key := fmt.Sprintf(redisKeyUserTraffic5min, user)

		// NX 确保当前 5 分钟 bucket 存在
		pipe.ZAddNX(ctx, key, goredis.Z{
			Score:  float64(scoreBase),
			Member: fmt.Sprintf("%d", ts),
		})

		// 增加 traffic（低 20bit 内累积）
		pipe.ZIncrBy(ctx, key, float64(trafficKB), fmt.Sprintf("%d", ts))

		// 清理旧数据
		pipe.ZRemRangeByScore(ctx, key, "0", fmt.Sprintf("%d", minScore))
	}

	// 汇总 all
	pipe.ZAddNX(ctx, redisKeyUserTraffic5minAll, goredis.Z{
		Score:  float64(scoreBase),
		Member: fmt.Sprintf("%d", ts),
	})
	pipe.ZIncrBy(ctx, redisKeyUserTraffic5minAll, float64(total), fmt.Sprintf("%d", ts))
	pipe.ZRemRangeByScore(ctx, redisKeyUserTraffic5minAll, "0", fmt.Sprintf("%d", minScore))

	_, err = pipe.Exec(ctx)
	return err
}

// 获取用户最近 N 小时的流量
func ListUserTrafficPer5Min(ctx context.Context, rdb *redis.Redis, userName string, hours int) (map[int64]int64, error) {
	now := time.Now()
	startTs := now.Add(-time.Hour*time.Duration(hours)).Unix() / fiveMinutes
	start := startTs << fiveMinDataBit
	stop := ((now.Unix() / fiveMinutes) + 1) << fiveMinDataBit
	key := fmt.Sprintf(redisKeyUserTraffic5min, userName)
	pairs, err := rdb.ZrangebyscoreWithScores(key, start, stop)
	if err != nil {
		return nil, err
	}

	res := make(map[int64]int64)
	for _, pair := range pairs {
		ts, _ := strconv.ParseInt(pair.Key, 10, 64)
		ts = ts * fiveMinutes
		res[ts] = pair.Score & ((1 << fiveMinDataBit) - 1)
	}

	return res, nil
}

// 获取全部用户最近 N 小时的流量
func ListAllTrafficPer5Min(ctx context.Context, rdb *redis.Redis, hours int) (map[int64]int64, error) {
	now := time.Now()
	startTs := now.Add(-time.Hour*time.Duration(hours)).Unix() / fiveMinutes
	start := startTs << fiveMinDataBit
	stop := ((now.Unix() / fiveMinutes) + 1) << fiveMinDataBit

	pairs, err := rdb.ZrangebyscoreWithScores(redisKeyUserTraffic5minAll, start, stop)
	if err != nil {
		return nil, err
	}

	res := make(map[int64]int64)
	for _, pair := range pairs {
		ts, _ := strconv.ParseInt(pair.Key, 10, 64)
		ts = ts * fiveMinutes
		res[ts] = int64(pair.Score) & ((1 << fiveMinDataBit) - 1)
	}

	return res, nil
}
