package logic

import (
	"context"
	"fmt"
	"sort"

	"titan-ipoverlay/ippop/rpc/serverapi"
	"titan-ipoverlay/manager/internal/svc"
	"titan-ipoverlay/manager/internal/types"
	"titan-ipoverlay/manager/model"

	"github.com/zeromicro/go-zero/core/logx"
)

type GetUserStatsChartLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewGetUserStatsChartLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetUserStatsChartLogic {
	return &GetUserStatsChartLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetUserStatsChartLogic) GetUserStatsChart(req *types.UserStatsChartReq) (resp *types.UserStatsChartResp, err error) {
	popIDs, err := model.GetUserPops(l.svcCtx.Redis, req.Username)
	if err != nil {
		return nil, fmt.Errorf("get user %s error %s", req.Username, err.Error())
	}

	if len(popIDs) == 0 {
		return nil, fmt.Errorf("user %s not exist", req.Username)
	}

	aggStats := make(map[int64]*types.StatPoint)
	for _, popID := range popIDs {
		server := l.svcCtx.Pops[popID]
		if server == nil {
			continue
		}
		userStatChartReq := &serverapi.UserStatChartReq{Username: req.Username, Type: req.Type, StartTime: req.StartTime, EndTime: req.EndTime}
		statsResp, err := server.API.GetUserStatChart(l.ctx, userStatChartReq)
		if err != nil {
			return nil, fmt.Errorf("get user stats chart failed:%v", err)
		}
		for _, statPoint := range statsResp.Stats {
			if existing, ok := aggStats[statPoint.Timestamp]; ok {
				existing.Bandwidth += statPoint.Bandwidth
				existing.Traffic += statPoint.Traffic
			} else {
				aggStats[statPoint.Timestamp] = &types.StatPoint{
					Timestamp: statPoint.Timestamp,
					Bandwidth: statPoint.Bandwidth,
					Traffic:   statPoint.Traffic,
				}
			}
		}
	}

	keys := make([]int64, 0, len(aggStats))
	for k := range aggStats {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })

	stats := make([]*types.StatPoint, 0, len(keys))
	for _, k := range keys {
		stats = append(stats, aggStats[k])
	}

	return &types.UserStatsChartResp{Stats: stats}, nil
}
