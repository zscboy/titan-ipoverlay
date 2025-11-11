package model

import "github.com/zeromicro/go-zero/core/stores/redis"

func AddBlacklist(redis *redis.Redis, nodeId string) error {
	_, err := redis.Sadd(redisKeyNodeBlacklist, nodeId)
	return err
}

func RemoveBlacklist(redis *redis.Redis, nodeId string) error {
	_, err := redis.Srem(redisKeyNodeBlacklist, nodeId)
	return err
}

func GetBlacklist(redis *redis.Redis) ([]string, error) {
	return redis.Smembers(redisKeyNodeBlacklist)
}

func IsBlacklisted(redis *redis.Redis, nodeId string) (bool, error) {
	return redis.Sismember(redisKeyNodeBlacklist, nodeId)
}
