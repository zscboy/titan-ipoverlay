package model

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/stores/redis"
)

// popAndIP = pop:ip, ip allow empty
func SetNodePopIP(rds *redis.Redis, nodeID, pop, ip string) error {
	popID, _, err := GetNodePopIP(rds, nodeID)
	if err != nil {
		return err
	}

	ctx := context.Background()
	pipe, err := rds.TxPipeline()
	if err != nil {
		return err
	}

	if len(popID) > 0 {
		if string(popID) != pop {
			pipe.SRem(ctx, fmt.Sprintf(redisKeyPopNodes, popID), nodeID)
			logx.Debugf("remove node %s from pop %s, add to new pop %s", nodeID, popID, pop)
		}
	}

	pipe.SAdd(ctx, fmt.Sprintf(redisKeyPopNodes, pop), nodeID)
	pipe.HSet(ctx, redisKeyNodes, nodeID, fmt.Sprintf("%s:%s", pop, ip))
	_, err = pipe.Exec(ctx)
	return err
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

func NodeCountOfPops(ctx context.Context, rds *redis.Redis, popIDs []string) (map[string]int64, error) {
	if len(popIDs) == 0 {
		return map[string]int64{}, nil
	}

	pipe, err := rds.TxPipeline()
	if err != nil {
		return nil, err
	}

	cmds := make(map[string]*redis.IntCmd, len(popIDs))

	for _, popID := range popIDs {
		key := fmt.Sprintf(redisKeyPopNodes, popID)
		cmds[popID] = pipe.SCard(ctx, key)
	}

	// 执行 pipeline
	if _, err := pipe.Exec(ctx); err != nil {
		return nil, err
	}

	// 读取结果
	result := make(map[string]int64, len(popIDs))
	for popID, cmd := range cmds {
		cnt, err := cmd.Result()
		if err != nil {
			return nil, err
		}
		result[popID] = cnt
	}

	return result, nil
}
