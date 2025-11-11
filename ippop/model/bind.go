package model

import (
	"context"
	"fmt"
	"time"

	goredis "github.com/redis/go-redis/v9"
	"github.com/zeromicro/go-zero/core/logx"
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

func BindNodeWithNewUser(ctx context.Context, redis *redis.Redis, nodeID string, user *User) error {
	isSuccess := false
	var n *Node = nil
	defer func() {
		if !isSuccess && n != nil && n.Online {
			AddFreeNode(redis, nodeID)
		}
	}()

	node, err := GetNode(ctx, redis, nodeID)
	if err != nil {
		return err
	}

	if node == nil {
		return fmt.Errorf("node %s not exist", nodeID)
	}

	n = node

	if len(node.BindUser) != 0 {
		return fmt.Errorf("node %s already  bind by user %s", nodeID, node.BindUser)
	}

	node.BindUser = user.UserName
	nodeKey := fmt.Sprintf(redisKeyNode, node.Id)
	nodeMap, err := structToMap(node)
	if err != nil {
		return err
	}

	userKey := fmt.Sprintf(redisKeyUser, user.UserName)
	userMap, err := structToMap(user)
	if err != nil {
		return err
	}

	pipe, err := redis.TxPipeline()
	if err != nil {
		return err
	}

	pipe.HSet(ctx, nodeKey, nodeMap)
	pipe.HSet(ctx, userKey, userMap)
	pipe.ZAdd(ctx, redisKeyUserZset, goredis.Z{Score: float64(time.Now().Unix()), Member: user.UserName})
	pipe.ZAdd(ctx, redisKeyNodeBind, goredis.Z{Score: float64(time.Now().Unix()), Member: nodeID})
	pipe.ZRem(ctx, redisKeyNodeFree, nodeID)

	_, err = pipe.Exec(ctx)
	if err != nil {
		return err
	}

	isSuccess = true
	return nil
}

func UnbindNode(ctx context.Context, redis *redis.Redis, nodeID string) error {
	node, err := GetNode(ctx, redis, nodeID)
	if err != nil {
		return err
	}

	if node == nil {
		return fmt.Errorf("node %s not exist", nodeID)
	}

	node.BindUser = ""
	key := fmt.Sprintf(redisKeyNode, node.Id)
	m, err := structToMap(node)
	if err != nil {
		return err
	}

	logx.Debugf("nodeM:%v", m)

	pipe, err := redis.TxPipeline()
	if err != nil {
		return err
	}

	pipe.HSet(ctx, key, m)
	pipe.ZRem(ctx, redisKeyNodeBind, nodeID)

	if node.Online {
		pipe.ZAdd(ctx, redisKeyNodeFree, goredis.Z{Score: float64(time.Now().Unix()), Member: nodeID})
	}

	_, err = pipe.Exec(ctx)
	if err != nil {
		return err
	}

	return nil
}

// if return err, need to add toNodeID to free node
func SwitchNodeByUser(ctx context.Context, redis *redis.Redis, user *User, toNodeID string) error {
	fromNodeID := user.RouteNodeID
	fromNode, err := GetNode(ctx, redis, fromNodeID)
	if err != nil {
		return err
	}

	if fromNode == nil {
		return fmt.Errorf("node %s not exist", fromNodeID)
	}

	toNode, err := GetNode(ctx, redis, toNodeID)
	if err != nil {
		return err
	}

	if toNode == nil {
		return fmt.Errorf("node %s not exist", toNodeID)
	}

	if len(toNode.BindUser) != 0 {
		return fmt.Errorf("node %s already bind by user %s", toNodeID, toNode.BindUser)
	}

	fromNode.BindUser = ""
	fromNodekey := fmt.Sprintf(redisKeyNode, fromNode.Id)
	fromM, err := structToMap(fromNode)
	if err != nil {
		return err
	}

	// logx.Debugf("fromNodekey:%s, fromM:%v", fromNodekey, fromM)

	toNode.BindUser = user.UserName
	toNodekey := fmt.Sprintf(redisKeyNode, toNode.Id)
	toM, err := structToMap(toNode)
	if err != nil {
		return err
	}

	// logx.Debugf("toM:%v", toM)

	user.RouteNodeID = toNode.Id
	user.LastRouteSwitchTime = time.Now().Unix()
	userKey := fmt.Sprintf(redisKeyUser, user.UserName)
	userMap, err := structToMap(user)
	if err != nil {
		return err
	}

	// logx.Debugf("userMap:%v", userMap)

	pipe, err := redis.TxPipeline()
	if err != nil {
		return err
	}

	// unbind from
	pipe.HSet(ctx, fromNodekey, fromM)
	pipe.ZRem(ctx, redisKeyNodeBind, fromNode.Id)

	if fromNode.Online {
		pipe.ZAdd(ctx, redisKeyNodeFree, goredis.Z{Score: float64(time.Now().Unix()), Member: fromNode.Id})
	}

	// bind to
	pipe.HSet(ctx, toNodekey, toM)
	pipe.HSet(ctx, userKey, userMap)
	pipe.ZAdd(ctx, redisKeyNodeBind, goredis.Z{Score: float64(time.Now().Unix()), Member: toNode.Id})
	pipe.ZRem(ctx, redisKeyNodeFree, toNode.Id)

	_, err = pipe.Exec(ctx)
	if err != nil {
		return err
	}

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
