package model

import (
	"time"

	"github.com/zeromicro/go-zero/core/stores/redis"
)

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

func GetBlacklistPage(redis *redis.Redis, cursor uint64, count int) ([]string, uint64, error) {
	return redis.Sscan(redisKeyNodeBlacklist, cursor, "", int64(count))
}

func ClearBlacklist(redis *redis.Redis) error {
	_, err := redis.Del(redisKeyNodeBlacklist)
	return err
}

var processExpiredScript = `
local expired = redis.call('ZRANGEBYSCORE', KEYS[1], '-inf', ARGV[1])
if #expired > 0 then
    redis.call('SREM', KEYS[2], unpack(expired))
    redis.call('ZREMRANGEBYSCORE', KEYS[1], '-inf', ARGV[1])
end
return expired
`

func ProcessExpiredProbations(r *redis.Redis) ([]string, error) {
	now := time.Now().Unix()
	resp, err := r.Eval(processExpiredScript, []string{"node:qos:probation", redisKeyNodeBlacklist}, now)
	if err != nil {
		// "redis: nil" error is returned when eval yields no list, but here we return empty table or nil.
		return nil, nil
	}

	var expiredIPs []string
	if arr, ok := resp.([]interface{}); ok {
		for _, v := range arr {
			if str, ok := v.(string); ok {
				expiredIPs = append(expiredIPs, str)
			}
		}
	}
	return expiredIPs, nil
}

func AddProbation(r *redis.Redis, ip string, expireTime int64) error {
	script := `return redis.call('ZADD', KEYS[1], ARGV[1], ARGV[2])`
	_, err := r.Eval(script, []string{"node:qos:probation"}, expireTime, ip)
	return err
}
