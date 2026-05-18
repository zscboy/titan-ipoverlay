package model

import (
	"errors"
	"strings"

	"github.com/zeromicro/go-zero/core/stores/redis"
)

func SetUserPop(redis *redis.Redis, user string, pops []string) error {
	if len(pops) == 0 {
		return nil
	}

	existingPops, err := GetUserPops(redis, user)
	if err != nil {
		return err
	}

	popMap := make(map[string]bool)
	for _, p := range existingPops {
		popMap[p] = true
	}

	for _, p := range pops {
		if !popMap[p] {
			existingPops = append(existingPops, p)
			popMap[p] = true
		}
	}

	newPopsStr := strings.Join(existingPops, ",")
	return redis.Hset(redisKeyUsers, user, newPopsStr)
}

func GetUserPops(red *redis.Redis, user string) ([]string, error) {
	popID, err := red.Hget(redisKeyUsers, user)
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return nil, nil
		}

		return nil, err
	}

	if len(popID) == 0 {
		return nil, nil
	}

	return strings.Split(popID, ","), nil
}

func DeleteUser(redis *redis.Redis, user string) error {
	_, err := redis.Hdel(redisKeyUsers, user)
	return err
}
