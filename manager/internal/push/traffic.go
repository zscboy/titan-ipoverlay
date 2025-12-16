package push

import (
	"context"
	"titan-ipoverlay/manager/internal/types"
	"titan-ipoverlay/manager/model"

	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/stores/redis"
)

type Traffic struct {
	redis *redis.Redis
}

func (t *Traffic) HandleStats(data any) error {
	traffics := data.([]*types.TrafficPoint)
	recores := make([]*model.TrafficRecore, 0, len(traffics))

	for _, tPoint := range traffics {
		if len(tPoint.Username) == 0 {
			logx.Error("handleTraffic, username empty")
			continue
		}

		if tPoint.Traffic <= 0 {
			logx.Errorf("handleTraffic, invalid traffic %d", tPoint.Traffic)
			continue
		}

		if !t.isValidUnixSec(tPoint.Timestamp) {
			logx.Errorf("handleTraffic, invalid Timestamp %d", tPoint.Timestamp)
			continue
		}

		recore := &model.TrafficRecore{
			Username:  tPoint.Username,
			Timestamp: tPoint.Timestamp,
			Value:     tPoint.Traffic,
		}
		recores = append(recores, recore)
	}

	if err := model.AddUsersTraffic5Minutes(context.Background(), t.redis, recores); err != nil {
		logx.Errorf("AddUsersTraffic5Minutes %v", err)
	}

	return nil
}

func (t *Traffic) isValidUnixSec(ts int64) bool {
	return ts > 0 && ts < 1e10
}
