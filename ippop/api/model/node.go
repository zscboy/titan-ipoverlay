package model

import (
	"context"
	"fmt"
	"time"

	goredis "github.com/redis/go-redis/v9"
	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/stores/redis"
)

const (
	TimeLayout = "2006-01-02 15:04:05 -0700 MST"
)

type Node struct {
	Id            string
	OS            string `redis:"os"`
	LoginAt       string `redis:"login_at"`
	RegisterAt    string `redis:"register_at"`
	Online        bool
	IP            string `redis:"ip"`
	BindUser      string `redis:"bind_user"`
	NetDelay      int64  `redis:"net_delay"`
	IsBlacklisted bool
}

func SetNodeAndZadd(ctx context.Context, redis *redis.Redis, node *Node) error {
	hashKey := fmt.Sprintf(redisKeyNode, node.Id)
	m, err := structToMap(node)
	if err != nil {
		return err
	}

	t, err := time.Parse(TimeLayout, node.RegisterAt)
	if err != nil {
		return err
	}

	pipe, err := redis.TxPipeline()
	if err != nil {
		return err
	}

	pipe.HMSet(ctx, hashKey, m)
	pipe.ZAdd(ctx, redisKeyNodeZset, goredis.Z{Score: float64(t.Unix()), Member: node.Id})

	_, err = pipe.Exec(ctx)
	if err != nil {
		return err
	}

	return err
}

func SetNodeNetDelay(redis *redis.Redis, nodeID string, delay uint64) error {
	node, _ := GetNode(redis, nodeID)
	if node == nil {
		return fmt.Errorf("SetNodeNetDelay node %s not exist", nodeID)
	}

	key := fmt.Sprintf(redisKeyNode, nodeID)
	return redis.Hset(key, "net_delay", fmt.Sprintf("%d", delay))
}

func SaveNode(redis *redis.Redis, node *Node) error {
	key := fmt.Sprintf(redisKeyNode, node.Id)
	m, err := structToMap(node)
	if err != nil {
		return err
	}

	logx.Infof("m:%v", m)
	return redis.Hmset(key, m)
}

// GetNode if node not exist, return nil
// need to check node if nil
func GetNode(redis *redis.Redis, id string) (*Node, error) {
	key := fmt.Sprintf(redisKeyNode, id)
	m, err := redis.Hgetall(key)
	if err != nil {
		return nil, err
	}

	if len(m) == 0 {
		return nil, nil
	}

	node := &Node{}
	err = mapToStruct(m, node)
	if err != nil {
		return nil, err
	}

	online, err := isNodeOnline(redis, id)
	if err != nil {
		return nil, err
	}

	isBlacklisted, err := IsBlacklisted(redis, id)
	if err != nil {
		return nil, err
	}

	node.Id = id
	node.Online = online
	node.IsBlacklisted = isBlacklisted
	return node, nil
}

func ListNode(ctx context.Context, redis *redis.Redis, start, end int) ([]*Node, error) {
	return listNode(ctx, redis, redisKeyNodeZset, start, end)
}

func ListFreeNode(ctx context.Context, redis *redis.Redis, start, end int) ([]*Node, error) {
	return listNode(ctx, redis, redisKeyNodeFree, start, end)
}

func ListBindNode(ctx context.Context, redis *redis.Redis, start, end int) ([]*Node, error) {
	return listNode(ctx, redis, redisKeyNodeBind, start, end)
}

func listNode(ctx context.Context, redis *redis.Redis, keyOfnodeSortSet string, start, end int) ([]*Node, error) {
	ids, err := redis.Zrange(keyOfnodeSortSet, int64(start), int64(end))
	if err != nil {
		return nil, err
	}

	return ListNodeWithIDs(ctx, redis, ids)
}

func ListNodeWithIDs(ctx context.Context, redis *redis.Redis, nodeIDs []string) ([]*Node, error) {
	onlines, err := getNodesOnlineStatus(ctx, redis, nodeIDs)
	if err != nil {
		return nil, err
	}

	blacklistStatus, err := getNodesBlacklistStatus(ctx, redis, nodeIDs)
	if err != nil {
		return nil, err
	}

	pipe, err := redis.TxPipeline()
	if err != nil {
		return nil, err
	}

	for _, id := range nodeIDs {
		key := fmt.Sprintf(redisKeyNode, id)
		pipe.HGetAll(ctx, key)
	}

	cmds, err := pipe.Exec(ctx)
	if err != nil {
		return nil, err
	}

	nodes := make([]*Node, 0, len(cmds))
	for i, cmd := range cmds {
		result, err := cmd.(*goredis.MapStringStringCmd).Result()
		if err != nil {
			logx.Errorf("ListNode parse result failed:%s", err.Error())
			continue
		}

		id := nodeIDs[i]
		node := Node{Id: id, Online: onlines[id], IsBlacklisted: blacklistStatus[id]}
		err = mapToStruct(result, &node)
		if err != nil {
			logx.Errorf("ListNode mapToStruct error:%s", err.Error())
			continue
		}

		nodes = append(nodes, &node)
	}

	return nodes, nil
}

func GetNodeLen(redis *redis.Redis) (int, error) {
	return redis.Zcard(redisKeyNodeZset)
}

func GetUnbindNodeLen(redis *redis.Redis) (int, error) {
	return redis.Zcard(redisKeyNodeFree)
}
func GetbindNodeLen(redis *redis.Redis) (int, error) {
	return redis.Zcard(redisKeyNodeBind)
}

func SetNodeOnline(redis *redis.Redis, nodeId string) error {
	if _, err := redis.Sadd(redisKeyNodeOnline, nodeId); err != nil {
		return err
	}

	return nil
}

func SetNodeOffline(redis *redis.Redis, nodeId string) error {
	_, err := redis.Srem(redisKeyNodeOnline, nodeId)
	return err
}

func isNodeOnline(redis *redis.Redis, nodeId string) (bool, error) {
	return redis.Sismember(redisKeyNodeOnline, nodeId)
}

func getNodesOnlineStatus(ctx context.Context, redis *redis.Redis, nodeIds []string) (map[string]bool, error) {
	pipe, err := redis.TxPipeline()
	if err != nil {
		return nil, err
	}

	for _, id := range nodeIds {
		pipe.SIsMember(ctx, redisKeyNodeOnline, id)
	}

	results, err := pipe.Exec(ctx)
	if err != nil {
		return nil, err
	}

	onlines := make(map[string]bool)
	for i, result := range results {
		exist, err := result.(*goredis.BoolCmd).Result()
		if err != nil {
			logx.Errorf("ListNode parse result failed:%s", err.Error())
			continue
		}
		onlines[nodeIds[i]] = exist
	}
	return onlines, nil
}

func getNodesBlacklistStatus(ctx context.Context, redis *redis.Redis, nodeIds []string) (map[string]bool, error) {
	pipe, err := redis.TxPipeline()
	if err != nil {
		return nil, err
	}

	for _, id := range nodeIds {
		pipe.SIsMember(ctx, redisKeyNodeBlacklist, id)
	}

	results, err := pipe.Exec(ctx)
	if err != nil {
		return nil, err
	}

	blacklistStatus := make(map[string]bool)
	for i, result := range results {
		exist, err := result.(*goredis.BoolCmd).Result()
		if err != nil {
			logx.Errorf("ListNode parse result failed:%s", err.Error())
			continue
		}
		blacklistStatus[nodeIds[i]] = exist
	}
	return blacklistStatus, nil
}

func DeleteNodeOnlineData(redis *redis.Redis) error {
	_, err := redis.Del(redisKeyNodeOnline)
	if err != nil {
		return err
	}

	_, err = redis.Del(redisKeyNodeFree)
	return err
}

func SetNodeOnlineDataExpire(redis *redis.Redis, seconds int) error {
	if err := redis.Expire(redisKeyNodeOnline, seconds); err != nil {
		return err
	}

	if err := redis.Expire(redisKeyNodeFree, seconds); err != nil {
		return err
	}

	return nil
}
