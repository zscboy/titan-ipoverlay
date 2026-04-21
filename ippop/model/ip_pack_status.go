package model

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"titan-ipoverlay/ippop/businesspack"

	"github.com/zeromicro/go-zero/core/stores/redis"
)

type IPPackStatus struct {
	Label        string `json:"label"`
	SuccessCount int64  `json:"success_count"`
	FailureCount int64  `json:"failure_count"`
	LastUpdated  int64  `json:"last_updated"`
}

func GetIPPackStatus(ctx context.Context, rds *redis.Redis, ip, pack string) (*IPPackStatus, error) {
	if ip == "" || pack == "" {
		return nil, nil
	}

	value, err := rds.Hget(fmt.Sprintf(redisKeyNodePackStatus, ip), pack)
	if err != nil {
		if err == redis.Nil {
			return nil, nil
		}
		return nil, err
	}
	if value == "" {
		return nil, nil
	}

	status := new(IPPackStatus)
	if err := json.Unmarshal([]byte(value), status); err != nil {
		return nil, err
	}
	return status, nil
}

func SaveIPPackStatus(ctx context.Context, rds *redis.Redis, ip, pack string, status *IPPackStatus) error {
	if ip == "" || pack == "" || status == nil {
		return nil
	}

	payload, err := json.Marshal(status)
	if err != nil {
		return err
	}
	return rds.Hset(fmt.Sprintf(redisKeyNodePackStatus, ip), pack, string(payload))
}

func ReportIPPackResult(ctx context.Context, rds *redis.Redis, ip, pack string, success bool) (*IPPackStatus, error) {
	status, err := GetIPPackStatus(ctx, rds, ip, pack)
	if err != nil {
		return nil, err
	}
	if status == nil {
		status = &IPPackStatus{Label: businesspack.LabelUnknown}
	}

	if success {
		status.SuccessCount++
	} else {
		status.FailureCount++
	}
	status.LastUpdated = time.Now().Unix()
	status.Label = DeriveIPPackLabel(status.SuccessCount, status.FailureCount)

	if err := SaveIPPackStatus(ctx, rds, ip, pack, status); err != nil {
		return nil, err
	}
	return status, nil
}

func DeriveIPPackLabel(successCount, failureCount int64) string {
	total := successCount + failureCount
	if total == 0 {
		return businesspack.LabelUnknown
	}

	if total < 3 {
		if failureCount == 0 {
			return businesspack.LabelAllow
		}
		return businesspack.LabelGray
	}

	failureRate := float64(failureCount) / float64(total)
	switch {
	case total >= 5 && failureRate >= 0.8:
		return businesspack.LabelDeny
	case failureRate >= 0.4:
		return businesspack.LabelGray
	default:
		return businesspack.LabelAllow
	}
}
