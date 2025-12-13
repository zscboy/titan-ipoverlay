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
	oneHourDataBit = 32     // 低 29 bit 存流量
	keepSevenDays  = 24 * 7 // 保留7天
	oneHour        = 1 * 60 * 60
)

func floorToHour(t time.Time) int64 {
	return t.Unix() / oneHour // oneHour = 5分钟
}

// 保存用户每1小时流量
func AddUsersTrafficOneHour(ctx context.Context, rdb *redis.Redis, users map[string]int64) error {
	now := time.Now()
	ts := floorToHour(now)            // 1小时粒度时间戳
	scoreBase := ts << oneHourDataBit // 高位存时间戳
	minScore := (now.Add(-time.Hour*keepSevenDays).Unix() / oneHour) << oneHourDataBit

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
		key := fmt.Sprintf(redisKeyUserTrafficHour, user)

		// NX 确保当前1小时bucket 存在
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
	pipe.ZAddNX(ctx, redisKeyUserTrafficHourAll, goredis.Z{
		Score:  float64(scoreBase),
		Member: fmt.Sprintf("%d", ts),
	})
	pipe.ZIncrBy(ctx, redisKeyUserTrafficHourAll, float64(total), fmt.Sprintf("%d", ts))
	pipe.ZRemRangeByScore(ctx, redisKeyUserTrafficHourAll, "0", fmt.Sprintf("%d", minScore))

	_, err = pipe.Exec(ctx)
	return err
}

// 获取用户最近 N 小时的流量
func ListUserTrafficPerHour(ctx context.Context, rdb *redis.Redis, userName string, startTime int64, endTime int64) (map[int64]int64, error) {
	if startTime < 0 || endTime < 0 {
		return nil, fmt.Errorf("invalid startTime and endTime")
	}

	start := (startTime / oneHour) << oneHourDataBit
	stop := ((endTime / oneHour) + 1) << oneHourDataBit
	// logx.Debugf("start:%d, stop:%d", start, stop)
	key := fmt.Sprintf(redisKeyUserTrafficHour, userName)
	pairs, err := rdb.ZrangebyscoreWithScores(key, start, stop)
	if err != nil {
		return nil, err
	}
	// logx.Debugf("ListUserTrafficPerHour:%#v", pairs)
	res := make(map[int64]int64)
	for _, pair := range pairs {
		ts, _ := strconv.ParseInt(pair.Key, 10, 64)
		ts = ts * oneHour
		res[ts] = int64(pair.Score) & ((1 << oneHourDataBit) - 1)
	}

	return res, nil
}

// 获取全部用户最近 N 小时的流量
func ListAllTrafficPerHour(ctx context.Context, rdb *redis.Redis, hours int) (map[int64]int64, error) {
	now := time.Now()
	startTs := now.Add(-time.Hour*time.Duration(hours)).Unix() / oneHour
	start := startTs << oneHourDataBit
	stop := ((now.Unix() / oneHour) + 1) << oneHourDataBit

	pairs, err := rdb.ZrangebyscoreWithScores(redisKeyUserTrafficHourAll, start, stop)
	if err != nil {
		return nil, err
	}

	res := make(map[int64]int64)
	for _, pair := range pairs {
		ts, _ := strconv.ParseInt(pair.Key, 10, 64)
		ts = ts * oneHour
		res[ts] = int64(pair.Score) & ((1 << oneHourDataBit) - 1)
	}

	return res, nil
}
