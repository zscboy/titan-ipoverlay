package model

import "github.com/zeromicro/go-zero/core/stores/redis"

func AddBlacklist(redis *redis.Redis, ips []string) error {
	if len(ips) == 0 {
		return nil
	}
	args := make([]any, len(ips))
	for i, ip := range ips {
		args[i] = ip
	}
	_, err := redis.Sadd(redisKeyNodeBlacklist, args...)
	return err
}

func RemoveBlacklist(redis *redis.Redis, ips []string) error {
	if len(ips) == 0 {
		return nil
	}
	args := make([]any, len(ips))
	for i, ip := range ips {
		args[i] = ip
	}
	_, err := redis.Srem(redisKeyNodeBlacklist, args...)
	return err
}

func GetBlacklist(redis *redis.Redis) ([]string, error) {
	var keys []string
	var cursor uint64
	for {
		res, nextCursor, err := redis.Sscan(redisKeyNodeBlacklist, cursor, "", 100)
		if err != nil {
			return nil, err
		}
		keys = append(keys, res...)
		cursor = nextCursor
		if cursor == 0 {
			break
		}
	}
	return keys, nil
}
