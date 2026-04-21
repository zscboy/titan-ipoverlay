package model

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/zeromicro/go-zero/core/stores/redis"
)

type IPPackStatus struct {
	Label        string `json:"label"`
	SuccessCount int64  `json:"success_count"`
	FailureCount int64  `json:"failure_count"`
	LastUpdated  int64  `json:"last_updated"`
}

func GetIPPackStatuses(ctx context.Context, rds *redis.Redis, ip string) (map[string]*IPPackStatus, error) {
	if ip == "" {
		return map[string]*IPPackStatus{}, nil
	}

	values, err := rds.Hgetall(fmt.Sprintf(redisKeyNodePackStatus, ip))
	if err != nil {
		return nil, err
	}

	result := make(map[string]*IPPackStatus, len(values))
	for pack, raw := range values {
		status := new(IPPackStatus)
		if err := json.Unmarshal([]byte(raw), status); err != nil {
			return nil, err
		}
		result[pack] = status
	}
	return result, nil
}
