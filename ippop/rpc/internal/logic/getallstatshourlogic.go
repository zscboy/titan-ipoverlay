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
	onehour = 1 * 60 * 60
)

type GetAllStatsHourLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewGetAllStatsHourLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetAllStatsHourLogic {
	return &GetAllStatsHourLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *GetAllStatsHourLogic) GetAllStatsHour(in *pb.AllStatsReq) (*pb.AllStatsResp, error) {
	if in.Hours == 0 {
		return nil, fmt.Errorf("request hours")
	}

	traffics, err := model.ListAllTrafficPerHour(l.ctx, l.svcCtx.Redis, int(in.Hours))
	if err != nil {
		return nil, err
	}

	logx.Debugf("traffics:%#v", traffics)

	start := time.Now().Add(-time.Hour*time.Duration(in.Hours)).Unix() / onehour
	end := time.Now().Unix() / onehour

	logx.Debugf("start:%d, end:%d", start, end)
	trafficCount := int64(0)
	count := in.Hours * 60 * 60 / onehour
	trendDatas := make([]*pb.TrendData, 0, count)
	for i := start; i <= end; i++ {
		ts := i * onehour
		value := traffics[ts]
		trafficCount += value
		trandData := &pb.TrendData{Timestamp: ts, Bandwidth: value / onehour, Traffic: trafficCount}
		trendDatas = append(trendDatas, trandData)
	}

	return &pb.AllStatsResp{TrendDatas: trendDatas}, nil
}
