package model

import (
	"github.com/zeromicro/go-zero/core/stores/redis"
)

func AddIPBlacklist(redis *redis.Redis, ips []string) error {
	if len(ips) == 0 {
		return nil
	}
	args := make([]any, len(ips))
	for i, ip := range ips {
		args[i] = ip
	}
	_, err := redis.Sadd(redisKeyIPBlacklist, args...)
	return err
}

func RemoveIPBlacklist(redis *redis.Redis, ips []string) error {
	if len(ips) == 0 {
		return nil
	}
	args := make([]any, len(ips))
	for i, ip := range ips {
		args[i] = ip
	}
	_, err := redis.Srem(redisKeyIPBlacklist, args...)
	return err
}

func IsIPBlacklisted(redis *redis.Redis, ip string) (bool, error) {
	return redis.Sismember(redisKeyIPBlacklist, ip)
}

func GetIPBlacklist(redis *redis.Redis, cursor uint64, count int) ([]string, uint64, int, error) {
	total, err := redis.Scard(redisKeyIPBlacklist)
	if err != nil {
		return nil, 0, 0, err
	}

	keys, nextCursor, err := redis.Sscan(redisKeyIPBlacklist, cursor, "", int64(count))
	if err != nil {
		return nil, 0, int(total), err
	}

	return keys, nextCursor, int(total), nil
}
