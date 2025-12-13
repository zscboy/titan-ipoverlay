package logic

import (
	"context"
	"fmt"

	"titan-ipoverlay/manager/internal/svc"
	"titan-ipoverlay/manager/internal/types"
	"titan-ipoverlay/manager/model"

	"github.com/zeromicro/go-zero/core/logx"
)

const (
	chartTypeMinute = "minute"
	charTypeHour    = "hour"
	chartTypeDay    = "day"

	fiveMinutes = 5 * 60
	oneHour     = 1 * 60 * 60
	oneDay      = 24 * 60 * 60
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

// func (l *GetUserStatsChartLogic) GetUserStatsChart(req *types.UserStatsChartReq) (resp *types.UserStatsChartResp, err error) {
// 	popID, err := model.GetUserPop(l.svcCtx.Redis, req.Username)
// 	if err != nil {
// 		return nil, fmt.Errorf("get user %s error %s", req.Username, err.Error())
// 	}

// 	if len(popID) == 0 {
// 		return nil, fmt.Errorf("user %s not exist", req.Username)
// 	}

// 	server := l.svcCtx.Pops[popID]
// 	if server == nil {
// 		return nil, fmt.Errorf("pop %s not exist", popID)
// 	}

// 	userStatChartReq := &serverapi.UserStatChartReq{Username: req.Username, Type: req.Type, StartTime: req.StartTime, EndTime: req.EndTime}
// 	statsResp, err := server.API.GetUserStatChart(l.ctx, userStatChartReq)
// 	if err != nil {
// 		return nil, fmt.Errorf("get all stats per 5min failed:%v", err)
// 	}

// 	stats := make([]*types.StatPoint, 0, len(statsResp.Stats))
// 	for _, statPonint := range statsResp.Stats {
// 		stat := &types.StatPoint{Timestamp: statPonint.Timestamp, Bandwidth: statPonint.Bandwidth, Traffic: statPonint.Traffic}
// 		stats = append(stats, stat)
// 	}

// 	return &types.UserStatsChartResp{Stats: stats}, nil
// }

func (l *GetUserStatsChartLogic) GetUserStatsChart(req *types.UserStatsChartReq) (resp *types.UserStatsChartResp, err error) {
	if req.StartTime < 0 || req.EndTime < 0 {
		return nil, fmt.Errorf("invalid params startTime or endTime")
	}

	switch req.Type {
	case chartTypeMinute:
		return l.GetUserStatChartForMinute(req)
	case charTypeHour:
		return l.GetUserStatChartForHour(req)
	case chartTypeDay:
		return l.GetUserStatChartForDay(req)
	}

	return nil, fmt.Errorf("invalid chart type %s", req.Type)
}

func (l *GetUserStatsChartLogic) GetUserStatChartForMinute(in *types.UserStatsChartReq) (*types.UserStatsChartResp, error) {
	traffics, err := model.ListUserTrafficPer5Min(l.ctx, l.svcCtx.Redis, in.Username, in.StartTime, in.EndTime)
	if err != nil {
		return nil, err
	}

	// logx.Debugf("traffics:%#v", traffics)

	start := in.StartTime / int64(fiveMinutes)
	end := in.EndTime / int64(fiveMinutes)

	// logx.Debugf("start:%d, end:%d", start, end)
	trafficCount := int64(0)
	count := int32(in.EndTime-in.StartTime) / int32(fiveMinutes)
	stats := make([]*types.StatPoint, 0, count)
	for i := start; i <= end; i++ {
		ts := i * int64(fiveMinutes)
		value := traffics[ts]
		trafficCount += value
		stat := &types.StatPoint{Timestamp: ts, Bandwidth: value / int64(fiveMinutes), Traffic: trafficCount}
		stats = append(stats, stat)
	}

	return &types.UserStatsChartResp{Stats: stats}, nil
}

func (l *GetUserStatsChartLogic) GetUserStatChartForHour(in *types.UserStatsChartReq) (*types.UserStatsChartResp, error) {
	traffics, err := model.ListUserTrafficPerHour(l.ctx, l.svcCtx.Redis, in.Username, in.StartTime, in.EndTime)
	if err != nil {
		return nil, err
	}

	// logx.Debugf("traffics:%#v", traffics)

	start := in.StartTime / int64(oneHour)
	end := in.EndTime / int64(oneHour)

	// logx.Debugf("start:%d, end:%d", start, end)
	trafficCount := int64(0)
	count := int32(in.EndTime-in.StartTime) / int32(oneHour)
	stats := make([]*types.StatPoint, 0, count)
	for i := start; i <= end; i++ {
		ts := i * int64(oneHour)
		value := traffics[ts]
		trafficCount += value
		stat := &types.StatPoint{Timestamp: ts, Bandwidth: value / int64(oneHour), Traffic: trafficCount}
		stats = append(stats, stat)
	}

	return &types.UserStatsChartResp{Stats: stats}, nil
}

func (l *GetUserStatsChartLogic) GetUserStatChartForDay(in *types.UserStatsChartReq) (*types.UserStatsChartResp, error) {
	traffics, err := model.ListUserTrafficPerHour(l.ctx, l.svcCtx.Redis, in.Username, in.StartTime, in.EndTime)
	if err != nil {
		return nil, err
	}

	// logx.Debugf("traffics:%#v", traffics)

	start := in.StartTime / int64(oneDay)
	end := in.EndTime / int64(oneDay)

	// logx.Debugf("start:%d, end:%d", start, end)
	trafficCount := int64(0)
	count := int32(in.EndTime-in.StartTime) / int32(oneDay)
	stats := make([]*types.StatPoint, 0, count)
	for i := start; i <= end; i++ {
		ts := i * int64(oneDay)
		value := traffics[ts]
		trafficCount += value
		stat := &types.StatPoint{Timestamp: ts, Bandwidth: value / int64(oneDay), Traffic: trafficCount}
		stats = append(stats, stat)
	}

	return &types.UserStatsChartResp{Stats: stats}, nil
}
