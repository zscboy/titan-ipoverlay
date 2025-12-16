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

type TrafficRecore struct {
	Username  string
	Timestamp int64
	Value     int64
}

func floorTo5Min(timestamp int64) int64 {
	return timestamp / fiveMinutes // fiveMinux = 5分钟
}

// 保存用户每 5 分钟流量
func AddUsersTraffic5Minutes(ctx context.Context, rdb *redis.Redis, traffics []*TrafficRecore) error {
	minScore := (time.Now().Add(-time.Hour*keep24Hours).Unix() / fiveMinutes) << fiveMinDataBit

	pipe, err := rdb.TxPipeline()
	if err != nil {
		return err
	}

	for _, traffic := range traffics {
		if traffic.Value <= 0 {
			continue
		}

		ts := floorTo5Min(traffic.Timestamp) // 5 分钟粒度时间戳
		scoreBase := ts << fiveMinDataBit    // 高位存时间戳

		key := fmt.Sprintf(redisKeyUserTraffic5min, traffic.Username)

		pipe.ZAddNX(ctx, key, goredis.Z{
			Score:  float64(scoreBase),
			Member: fmt.Sprintf("%d", ts),
		})

		// 增加 traffic（低 fiveMinDataBit 内累积）
		trafficKB := traffic.Value / KB
		pipe.ZIncrBy(ctx, key, float64(trafficKB), fmt.Sprintf("%d", ts))

		// 清理旧数据
		pipe.ZRemRangeByScore(ctx, key, "0", fmt.Sprintf("%d", minScore))
	}

	_, err = pipe.Exec(ctx)
	return err
}

// 获取用户最近 N 小时的流量
// startTime, endTime 是时间戳
func ListUserTrafficPer5Min(ctx context.Context, rdb *redis.Redis, usernmae string, startTime int64, endTime int64) (map[int64]int64, error) {
	if startTime < 0 || endTime < 0 {
		return nil, fmt.Errorf("invalid startTime and endTime")
	}

	start := (startTime / fiveMinutes) << fiveMinDataBit
	stop := ((endTime / fiveMinutes) + 1) << fiveMinDataBit
	key := fmt.Sprintf(redisKeyUserTraffic5min, usernmae)
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

// return timastamp, traffic, err
func GetUserLastTrafficPer5Min(rdb *redis.Redis, username string) (int64, int64, error) {
	key := fmt.Sprintf(redisKeyUserTraffic5min, username)
	pairs, err := rdb.ZrevrangeWithScores(key, 0, 0)
	if err != nil {
		return 0, 0, err
	}

	if len(pairs) > 0 {
		ts, _ := strconv.ParseInt(pairs[0].Key, 10, 64)
		ts = ts * fiveMinutes
		traffic := int64(pairs[0].Score) & ((1 << fiveMinDataBit) - 1)
		return ts, traffic, nil
	}

	return 0, 0, nil
}
