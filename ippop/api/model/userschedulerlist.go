package model

import (
	"time"

	"github.com/zeromicro/go-zero/core/stores/redis"
)

func AddUserToSchedulerList(redis *redis.Redis, userName string) error {
	_, err := redis.Zadd(redisKeyUserRouteScheduler, time.Now().Unix(), userName)
	return err
}

func RemoveUserFromSchedulerList(redis *redis.Redis, userName string) error {
	_, err := redis.Zrem(redisKeyUserRouteScheduler, userName)
	return err
}

func ListUserFromSchedulerList(redis *redis.Redis, start, end int) ([]string, error) {
	userNames, err := redis.Zrevrange(redisKeyUserZset, int64(start), int64(end))
	if err != nil {
		return nil, err
	}
	return userNames, nil
}
