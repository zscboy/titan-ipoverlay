package logic

import (
	"context"
	"fmt"
	"time"

	"titan-ipoverlay/ippop/model"
	"titan-ipoverlay/ippop/rpc/internal/svc"
	"titan-ipoverlay/ippop/rpc/pb"

	"github.com/zeromicro/go-zero/core/logx"
)

const (
	oneDay = 24 * 60 * 60
)

type GetAllStatsDayLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewGetAllStatsDayLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetAllStatsDayLogic {
	return &GetAllStatsDayLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *GetAllStatsDayLogic) GetAllStatsDay(in *pb.AllStatsDayReq) (*pb.AllStatsResp, error) {
	if in.Days == 0 {
		return nil, fmt.Errorf("request hours")
	}

	traffics, err := model.ListAllTrafficPerDay(l.ctx, l.svcCtx.Redis, int(in.Days))
	if err != nil {
		return nil, err
	}

	logx.Debugf("traffics:%#v", traffics)

	start := time.Now().Add(-time.Hour*time.Duration(in.Days*24)).Unix() / oneDay
	end := time.Now().Unix() / oneDay

	logx.Debugf("start:%d, end:%d", start, end)
	trafficCount := int64(0)
	count := in.Days * 60 * 60 / oneDay
	trendDatas := make([]*pb.TrendData, 0, count)
	for i := start; i <= end; i++ {
		ts := i * oneDay
		value := traffics[ts]
		trafficCount += value
		trandData := &pb.TrendData{Timestamp: ts, Bandwidth: value / oneDay, Traffic: trafficCount}
		trendDatas = append(trendDatas, trandData)
	}

	return &pb.AllStatsResp{TrendDatas: trendDatas}, nil
}
