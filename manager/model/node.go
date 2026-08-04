package model

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/stores/redis"
)

const batchSize = 1000

// popAndIP = pop:ip:countryCode, ip and countryCode allow empty
func SetNodePopIP(rds *redis.Redis, nodeID, pop, ip, countryCode string) error {
	popID, _, _, err := GetNodePopIP(rds, nodeID)
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
	
	val := fmt.Sprintf("%s:%s", pop, ip)
	if len(countryCode) > 0 {
		val = fmt.Sprintf("%s:%s:%s", pop, ip, strings.ToLower(countryCode))
	}
	pipe.HSet(ctx, redisKeyNodes, nodeID, val)
	_, err = pipe.Exec(ctx)
	return err
}

// GetNodePopIP returns (popID, ip, countryCode, error) for the given nodeID
func GetNodePopIP(red *redis.Redis, nodeID string) ([]byte, []byte, []byte, error) {
	popAndIP, err := red.Hget(redisKeyNodes, nodeID)
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return nil, nil, nil, nil
		}

		return nil, nil, nil, err
	}

	vs := strings.Split(popAndIP, ":")
	if len(vs) > 2 {
		return []byte(vs[0]), []byte(vs[1]), []byte(vs[2]), nil
	}
	if len(vs) > 1 {
		return []byte(vs[0]), []byte(vs[1]), nil, nil
	}

	return []byte(vs[0]), nil, nil, nil
}

// GetNodePopIPs returns a map of nodeID -> popID using a batch query (Hmget)
func GetNodePopIPs(red *redis.Redis, nodeIDs []string) (map[string]string, error) {
	if len(nodeIDs) == 0 {
		return nil, nil
	}

	vals, err := red.Hmget(redisKeyNodes, nodeIDs...)
	if err != nil {
		return nil, err
	}

	nodePopMap := make(map[string]string)
	for i, val := range vals {
		if len(val) > 0 {
			vs := strings.Split(val, ":")
			if len(vs) > 0 && len(vs[0]) > 0 {
				nodePopMap[nodeIDs[i]] = vs[0]
			}
		}
	}

	return nodePopMap, nil
}

func DeleteNode(redis *redis.Redis, nodeID string) error {
	popID, _, _, err := GetNodePopIP(redis, nodeID)
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

func BatchMoveNodesToPop(rds *redis.Redis, nodeIDToIP map[string]string, sourcePop, targetPop string) error {
	if len(nodeIDToIP) == 0 {
		return nil
	}

	nodeIDs := make([]string, 0, len(nodeIDToIP))
	for nodeID := range nodeIDToIP {
		nodeIDs = append(nodeIDs, nodeID)
	}

	// Get existing country codes to retain them (batch query, max 1000 per batch)
	countryCodes := make(map[string]string)
	for i := 0; i < len(nodeIDs); i += batchSize {
		end := i + batchSize
		if end > len(nodeIDs) {
			end = len(nodeIDs)
		}
		batchNodeIDs := nodeIDs[i:end]
		vals, err := rds.Hmget(redisKeyNodes, batchNodeIDs...)
		if err != nil {
			return err
		}
		for j, val := range vals {
			if len(val) > 0 {
				vs := strings.Split(val, ":")
				if len(vs) > 2 {
					nodeID := batchNodeIDs[j]
					countryCodes[nodeID] = vs[2]
				}
			}
		}
	}

	ctx := context.Background()
	pipe, err := rds.TxPipeline()
	if err != nil {
		return err
	}

	if len(sourcePop) > 0 {
		pipe.SRem(ctx, fmt.Sprintf(redisKeyPopNodes, sourcePop), sliceToInterface(nodeIDs)...)
	}

	pipe.SAdd(ctx, fmt.Sprintf(redisKeyPopNodes, targetPop), sliceToInterface(nodeIDs)...)

	fields := make(map[string]interface{}, len(nodeIDToIP))
	for nodeID, ip := range nodeIDToIP {
		countryCode := countryCodes[nodeID]
		if len(countryCode) > 0 {
			fields[nodeID] = fmt.Sprintf("%s:%s:%s", targetPop, ip, strings.ToLower(countryCode))
		} else {
			fields[nodeID] = fmt.Sprintf("%s:%s", targetPop, ip)
		}
	}
	pipe.HMSet(ctx, redisKeyNodes, fields)

	_, err = pipe.Exec(ctx)
	return err
}

func sliceToInterface(ss []string) []interface{} {
	is := make([]interface{}, len(ss))
	for i, s := range ss {
		is[i] = s
	}
	return is
}
