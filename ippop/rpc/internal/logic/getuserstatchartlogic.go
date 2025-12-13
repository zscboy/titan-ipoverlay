package logic

import (
	"context"
	"fmt"

	"titan-ipoverlay/ippop/model"
	"titan-ipoverlay/ippop/rpc/internal/svc"
	"titan-ipoverlay/ippop/rpc/pb"

	"github.com/zeromicro/go-zero/core/logx"
)

const (
	chartTypeMinute = "minute"
	charTypeHour    = "hour"
	chartTypeDay    = "day"
)

type GetUserStatChartLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
	fiveMinutes int32
	oneHour     int32
	oneDay      int32
}

func NewGetUserStatChartLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetUserStatChartLogic {
	return &GetUserStatChartLogic{
		ctx:         ctx,
		svcCtx:      svcCtx,
		Logger:      logx.WithContext(ctx),
		fiveMinutes: 5 * 60,
		oneHour:     1 * 60 * 60,
		oneDay:      24 * 60 * 60,
	}
}

func (l *GetUserStatChartLogic) GetUserStatChart(in *pb.UserStatChartReq) (*pb.UserStatChartResp, error) {
	if in.StartTime < 0 || in.EndTime < 0 {
		return nil, fmt.Errorf("invalid params startTime or endTime")
	}

	switch in.Type {
	case chartTypeMinute:
		return l.GetUserStatChartForMinute(in)
	case charTypeHour:
		return l.GetUserStatChartForHour(in)
	case chartTypeDay:
		return l.GetUserStatChartForDay(in)
	}

	return nil, fmt.Errorf("invalid chart type %s", in.Type)
}

func (l *GetUserStatChartLogic) GetUserStatChartForMinute(in *pb.UserStatChartReq) (*pb.UserStatChartResp, error) {
	traffics, err := model.ListUserTrafficPer5Min(l.ctx, l.svcCtx.Redis, in.Username, in.StartTime, in.EndTime)
	if err != nil {
		return nil, err
	}

	// logx.Debugf("traffics:%#v", traffics)

	start := in.StartTime / int64(l.fiveMinutes)
	end := in.EndTime / int64(l.fiveMinutes)

	// logx.Debugf("start:%d, end:%d", start, end)
	trafficCount := int64(0)
	count := int32(in.EndTime-in.StartTime) / int32(l.fiveMinutes)
	stats := make([]*pb.StatPoint, 0, count)
	for i := start; i <= end; i++ {
		ts := i * int64(l.fiveMinutes)
		value := traffics[ts]
		trafficCount += value
		stat := &pb.StatPoint{Timestamp: ts, Bandwidth: value / int64(l.fiveMinutes), Traffic: trafficCount}
		stats = append(stats, stat)
	}

	return &pb.UserStatChartResp{Stats: stats}, nil
}

func (l *GetUserStatChartLogic) GetUserStatChartForHour(in *pb.UserStatChartReq) (*pb.UserStatChartResp, error) {
	traffics, err := model.ListUserTrafficPerHour(l.ctx, l.svcCtx.Redis, in.Username, in.StartTime, in.EndTime)
	if err != nil {
		return nil, err
	}

	// logx.Debugf("traffics:%#v", traffics)

	start := in.StartTime / int64(l.oneHour)
	end := in.EndTime / int64(l.oneHour)

	// logx.Debugf("start:%d, end:%d", start, end)
	trafficCount := int64(0)
	count := int32(in.EndTime-in.StartTime) / int32(l.oneHour)
	stats := make([]*pb.StatPoint, 0, count)
	for i := start; i <= end; i++ {
		ts := i * int64(l.oneHour)
		value := traffics[ts]
		trafficCount += value
		stat := &pb.StatPoint{Timestamp: ts, Bandwidth: value / int64(l.oneHour), Traffic: trafficCount}
		stats = append(stats, stat)
	}

	return &pb.UserStatChartResp{Stats: stats}, nil
}

func (l *GetUserStatChartLogic) GetUserStatChartForDay(in *pb.UserStatChartReq) (*pb.UserStatChartResp, error) {
	traffics, err := model.ListUserTrafficPerHour(l.ctx, l.svcCtx.Redis, in.Username, in.StartTime, in.EndTime)
	if err != nil {
		return nil, err
	}

	// logx.Debugf("traffics:%#v", traffics)

	start := in.StartTime / int64(l.oneDay)
	end := in.EndTime / int64(l.oneDay)

	// logx.Debugf("start:%d, end:%d", start, end)
	trafficCount := int64(0)
	count := int32(in.EndTime-in.StartTime) / int32(l.oneDay)
	stats := make([]*pb.StatPoint, 0, count)
	for i := start; i <= end; i++ {
		ts := i * int64(l.oneDay)
		value := traffics[ts]
		trafficCount += value
		stat := &pb.StatPoint{Timestamp: ts, Bandwidth: value / int64(l.oneDay), Traffic: trafficCount}
		stats = append(stats, stat)
	}

	return &pb.UserStatChartResp{Stats: stats}, nil
}
