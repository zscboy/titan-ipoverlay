package model

import (
	"errors"
	"fmt"
	"strings"

	"github.com/zeromicro/go-zero/core/stores/redis"
)

// popAndIP = pop:ip, ip allow empty
func SetNodePopIP(redis *redis.Redis, nodeID, pop, ip string) error {
	popID, _, err := GetNodePopIP(redis, nodeID)
	if err != nil {
		return err
	}

	if len(popID) > 0 {
		if string(popID) != pop {
			_, err = redis.Srem(fmt.Sprintf(redisKeyPopNodes, popID), nodeID)
			if err != nil {
				return err
			}
		}
	}

	_, err = redis.Sadd(fmt.Sprintf(redisKeyPopNodes, pop), nodeID)
	if err != nil {
		return err
	}
	return redis.Hset(redisKeyNodes, nodeID, fmt.Sprintf("%s:%s", pop, ip))
}

func GetNodePopIP(red *redis.Redis, nodeID string) ([]byte, []byte, error) {
	popAndIP, err := red.Hget(redisKeyNodes, nodeID)
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return nil, nil, nil
		}

		return nil, nil, err
	}

	vs := strings.Split(popAndIP, ":")
	if len(vs) > 1 {
		return []byte(vs[0]), []byte(vs[1]), nil
	}

	return []byte(vs[0]), nil, nil
}

func DeleteNode(redis *redis.Redis, nodeID string) error {
	popID, _, err := GetNodePopIP(redis, nodeID)
	if err != nil {
		return err
	}

	if len(popID) == 0 {
		return fmt.Errorf("node %s not exist", nodeID)
	}

	_, err = redis.Hdel(redisKeyNodes, nodeID)
	if err != nil {
		return err
	}

	_, err = redis.Srem(fmt.Sprintf(redisKeyPopNodes, string(popID)), nodeID)
	return err
}

func NodeCountOfPop(redis *redis.Redis, popID string) (int64, error) {
	return redis.Scard(fmt.Sprintf(redisKeyPopNodes, popID))
}
