package model

import (
	"context"
	"fmt"
	"strconv"
	"time"

	goredis "github.com/redis/go-redis/v9"
	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/stores/redis"
)

const (
	oneDayDataBit = 37      // 低 29 bit 存流量
	keepThirtyDay = 24 * 30 // 保留7天
	oneDay        = 24 * 60 * 60
)

func floorToDay(t time.Time) int64 {
	return t.Unix() / oneDay // oneDay = 5分钟
}

// 保存用户每 5 分钟流量
func AddUsersTrafficOneDay(ctx context.Context, rdb *redis.Redis, users map[string]int64) error {
	now := time.Now()
	ts := floorToDay(now)            // 5 分钟粒度时间戳
	scoreBase := ts << oneDayDataBit // 高位存时间戳
	minScore := (now.Add(-time.Hour*keepSevenDays).Unix() / oneDay) << oneDayDataBit

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
		key := fmt.Sprintf(redisKeyUserTrafficDay, user)

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
	pipe.ZAddNX(ctx, redisKeyUserTrafficDayAll, goredis.Z{
		Score:  float64(scoreBase),
		Member: fmt.Sprintf("%d", ts),
	})
	pipe.ZIncrBy(ctx, redisKeyUserTrafficDayAll, float64(total), fmt.Sprintf("%d", ts))
	pipe.ZRemRangeByScore(ctx, redisKeyUserTrafficDayAll, "0", fmt.Sprintf("%d", minScore))

	_, err = pipe.Exec(ctx)
	return err
}

// 获取用户最近 N 小时的流量
func ListUserTrafficPerDay(ctx context.Context, rdb *redis.Redis, userName string, startTime, endTime int64) (map[int64]int64, error) {
	if startTime < 0 || endTime < 0 {
		return nil, fmt.Errorf("invalid startTime and endTime")
	}

	start := (startTime / oneDay) << oneDayDataBit
	stop := ((endTime / oneDay) + 1) << oneDayDataBit
	logx.Debugf("start:%d, stop:%d", start, stop)
	key := fmt.Sprintf(redisKeyUserTrafficDay, userName)
	pairs, err := rdb.ZrangebyscoreWithScores(key, start, stop)
	if err != nil {
		return nil, err
	}
	logx.Debugf("ListUserTrafficPerHour:%#v", pairs)
	res := make(map[int64]int64)
	for _, pair := range pairs {
		ts, _ := strconv.ParseInt(pair.Key, 10, 64)
		ts = ts * oneDay
		res[ts] = int64(pair.Score) & ((1 << oneDayDataBit) - 1)
	}

	return res, nil
}

// 获取全部用户最近 N 小时的流量
func ListAllTrafficPerDay(ctx context.Context, rdb *redis.Redis, days int) (map[int64]int64, error) {
	now := time.Now()
	startTs := now.Add(-time.Hour*time.Duration(days*24)).Unix() / oneDay
	start := startTs << oneDayDataBit
	stop := ((now.Unix() / oneDay) + 1) << oneDayDataBit
	logx.Debugf("start:%d, stop:%d", start, stop)
	pairs, err := rdb.ZrangebyscoreWithScores(redisKeyUserTrafficDayAll, start, stop)
	if err != nil {
		return nil, err
	}
	logx.Debugf("ListUserTrafficPerHour:%#v", pairs)
	res := make(map[int64]int64)
	for _, pair := range pairs {
		ts, _ := strconv.ParseInt(pair.Key, 10, 64)
		ts = ts * oneDay
		res[ts] = int64(pair.Score) & ((1 << oneDayDataBit) - 1)
	}

	return res, nil
}
