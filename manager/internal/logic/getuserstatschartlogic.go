package logic

import (
	"context"
	"fmt"

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
	popID, err := model.GetUserPop(l.svcCtx.Redis, req.Username)
	if err != nil {
		return nil, fmt.Errorf("get user %s error %s", req.Username, err.Error())
	}

	if len(popID) == 0 {
		return nil, fmt.Errorf("user %s not exist", req.Username)
	}

	server := l.svcCtx.Pops[popID]
	if server == nil {
		return nil, fmt.Errorf("pop %s not exist", popID)
	}

	userStatChartReq := &serverapi.UserStatChartReq{Username: req.Username, Type: req.Type, StartTime: req.StartTime, EndTime: req.EndTime}
	statsResp, err := server.API.GetUserStatChart(l.ctx, userStatChartReq)
	if err != nil {
		return nil, fmt.Errorf("get all stats per 5min failed:%v", err)
	}

	stats := make([]*types.StatPoint, 0, len(statsResp.Stats))
	for _, statPonint := range statsResp.Stats {
		stat := &types.StatPoint{Timestamp: statPonint.Timestamp, Bandwidth: statPonint.Bandwidth, Traffic: statPonint.Traffic}
		stats = append(stats, stat)
	}

	return &types.UserStatsChartResp{Stats: stats}, nil
}
