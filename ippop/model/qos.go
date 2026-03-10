package model

import (
	"fmt"
	"time"

	"github.com/zeromicro/go-zero/core/stores/redis"
)

// AddNodeStrike 增加节点的 Strike 计数，并返回增加后的值
func AddNodeStrike(r *redis.Redis, nodeID string) (int, error) {
	key := fmt.Sprintf(redisKeyNodeStrike, nodeID)
	strike, err := r.Incr(key)
	if err != nil {
		return 0, err
	}
	// 设置 24 小时过期
	err = r.Expire(key, 24*3600)
	return int(strike), err
}

// ClearNodeStrike 清除节点的 Strike 计数
func ClearNodeStrike(r *redis.Redis, nodeID string) error {
	key := fmt.Sprintf(redisKeyNodeStrike, nodeID)
	_, err := r.Del(key)
	return err
}

// GetNodeStrike 获取节点的 Strike 计数
func GetNodeStrike(r *redis.Redis, nodeID string) (int, error) {
	key := fmt.Sprintf(redisKeyNodeStrike, nodeID)
	val, err := r.Get(key)
	if err != nil {
		return 0, err
	}
	if val == "" {
		return 0, nil
	}
	var strike int
	fmt.Sscanf(val, "%d", &strike)
	return strike, nil
}

// AddBlacklistAudit 记录黑名单变更审计日志
func AddBlacklistAudit(r *redis.Redis, nodeID, ip, action, reason string) error {
	logEntry := fmt.Sprintf("%s | Node: %s | IP: %s | Action: %s | Reason: %s",
		time.Now().Format("2006-01-02 15:04:05"), nodeID, ip, action, reason)

	// 推送到列表头部
	_, err := r.Lpush(redisKeyNodeBlacklistAudit, logEntry)
	if err != nil {
		return err
	}

	// 只保留最近 1000 条
	return r.Ltrim(redisKeyNodeBlacklistAudit, 0, 999)
}

// GetBlacklistAudits 获取审计日志列表
func GetBlacklistAudits(r *redis.Redis, count int) ([]string, error) {
	return r.Lrange(redisKeyNodeBlacklistAudit, 0, count-1)
}
