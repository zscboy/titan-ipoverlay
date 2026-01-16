package model

import (
	"context"
	"fmt"
	"time"

	goredis "github.com/redis/go-redis/v9"
	"github.com/zeromicro/go-zero/core/stores/redis"
)

func RemoveFreeNode(redis *redis.Redis, nodeID string) error {
	_, err := redis.Zrem(redisKeyNodeFree, nodeID)
	return err
}

func AddFreeNode(redis *redis.Redis, nodeID string) error {
	_, err := redis.Zadd(redisKeyNodeFree, time.Now().Unix(), nodeID)
	return err
}

func AddFreeNodes(ctx context.Context, redis *redis.Redis, nodeIDs []string) error {
	if len(nodeIDs) == 0 {
		return nil
	}
	pipe, err := redis.TxPipeline()
	if err != nil {
		return err
	}
	score := float64(time.Now().Unix())
	for _, id := range nodeIDs {
		pipe.ZAdd(ctx, redisKeyNodeFree, goredis.Z{Score: score, Member: id})
	}
	_, err = pipe.Exec(ctx)
	return err
}

func BindNodeWithNewUser(ctx context.Context, rds *redis.Redis, nodeID string, user *User) error {
	now := time.Now().Unix()
	userKey := fmt.Sprintf(redisKeyUser, user.UserName)
	nodeKey := fmt.Sprintf(redisKeyNode, nodeID)

	pipe, err := rds.TxPipeline()
	if err != nil {
		return err
	}

	pipe.HSet(ctx, nodeKey, "bind_user", user.UserName)
	pipe.HSet(ctx, userKey, "route_node_id", nodeID, "last_route_switch_time", now)
	pipe.ZAdd(ctx, redisKeyUserZset, goredis.Z{Score: float64(now), Member: user.UserName})
	pipe.ZAdd(ctx, redisKeyNodeBind, goredis.Z{Score: float64(now), Member: nodeID})
	// Try to remove from free anyway, even if not there
	pipe.ZRem(ctx, redisKeyNodeFree, nodeID)

	_, err = pipe.Exec(ctx)
	if err != nil {
		return err
	}

	user.RouteNodeID = nodeID
	user.LastRouteSwitchTime = now
	return nil
}

func UnbindNode(ctx context.Context, rds *redis.Redis, nodeID string) error {
	now := time.Now().Unix()
	key := fmt.Sprintf(redisKeyNode, nodeID)

	pipe, err := rds.TxPipeline()
	if err != nil {
		return err
	}

	pipe.HSet(ctx, key, "bind_user", "")
	pipe.ZRem(ctx, redisKeyNodeBind, nodeID)
	sisMemberCmd := pipe.SIsMember(ctx, redisKeyNodeOnline, nodeID)

	_, err = pipe.Exec(ctx)
	if err != nil {
		return err
	}

	if isOnline, _ := sisMemberCmd.Result(); isOnline {
		// We still need to check if it's already in free pool?
		// Actually ZAdd will just update score.
		rds.Zadd(redisKeyNodeFree, now, nodeID)
	}

	return nil
}

// SwitchNodeByUser switches a user's route node.
func SwitchNodeByUser(ctx context.Context, rds *redis.Redis, user *User, toNodeID string) error {
	fromNodeID := user.RouteNodeID
	if fromNodeID == toNodeID {
		return nil
	}

	now := time.Now().Unix()
	username := user.UserName
	userKey := fmt.Sprintf(redisKeyUser, username)
	toNodeKey := fmt.Sprintf(redisKeyNode, toNodeID)

	// Step 1: Check if toNode exists and is free in one pipeline
	// Or even better, we can assume the caller has checked or we check as part of a transaction.
	// However, without Lua, we still need to check something if we want to be safe.
	// But the user's request is to SIMPLIFY and REDUCE operations.

	pipe, err := rds.TxPipeline()
	if err != nil {
		return err
	}

	// 1. Update User
	pipe.HSet(ctx, userKey, "route_node_id", toNodeID, "last_route_switch_time", now)

	// 2. Unbind Old Node
	var sisMemberCmd *goredis.BoolCmd
	if fromNodeID != "" {
		fromNodeKey := fmt.Sprintf(redisKeyNode, fromNodeID)
		pipe.HSet(ctx, fromNodeKey, "bind_user", "")
		pipe.ZRem(ctx, redisKeyNodeBind, fromNodeID)
		sisMemberCmd = pipe.SIsMember(ctx, redisKeyNodeOnline, fromNodeID)
	}

	// 3. Bind New Node
	pipe.HSet(ctx, toNodeKey, "bind_user", username)
	pipe.ZAdd(ctx, redisKeyNodeBind, goredis.Z{Score: float64(now), Member: toNodeID})
	pipe.ZRem(ctx, redisKeyNodeFree, toNodeID)

	_, err = pipe.Exec(ctx)
	if err != nil {
		return err
	}

	// 4. Post-check for old node online status to move it to free pool
	if sisMemberCmd != nil {
		if isOnline, _ := sisMemberCmd.Result(); isOnline {
			rds.Zadd(redisKeyNodeFree, now, fromNodeID)
		}
	}

	user.RouteNodeID = toNodeID
	user.LastRouteSwitchTime = now
	return nil
}

// return free node id and delete it from redisKeyNodeFree
func AllocateFreeNode(ctx context.Context, redis *redis.Redis) ([]byte, error) {
	pipe, err := redis.TxPipeline()
	if err != nil {
		return nil, err
	}

	pipe.ZPopMin(ctx, redisKeyNodeFree)

	cmds, err := pipe.Exec(ctx)
	if err != nil {
		return nil, err
	}

	for _, cmd := range cmds {
		result, err := cmd.(*goredis.ZSliceCmd).Result()
		if err != nil {
			return nil, err
		}

		if len(result) > 0 {
			return []byte(result[0].Member.(string)), nil
		}

	}
	return nil, fmt.Errorf("no free node found")
}
