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
	fiveMinutes = 300
)

type GetAllStats5MinLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewGetAllStats5MinLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetAllStats5MinLogic {
	return &GetAllStats5MinLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *GetAllStats5MinLogic) GetAllStats5Min(in *pb.AllStatsReq) (*pb.AllStatsResp, error) {
	if in.Hours == 0 {
		return nil, fmt.Errorf("request hours")
	}

	traffics, err := model.ListAllTrafficPer5Min(l.ctx, l.svcCtx.Redis, int(in.Hours))
	if err != nil {
		return nil, err
	}

	logx.Debugf("traffics:%#v", traffics)

	start := time.Now().Add(-time.Hour*time.Duration(in.Hours)).Unix() / fiveMinutes
	end := time.Now().Unix() / fiveMinutes

	logx.Debugf("start:%d, end:%d", start, end)
	trafficCount := int64(0)
	count := in.Hours * 60 * 60 / fiveMinutes
	trendDatas := make([]*pb.TrendData, 0, count)
	for i := start; i <= end; i++ {
		ts := i * fiveMinutes
		value := traffics[ts]
		trafficCount += value
		trandData := &pb.TrendData{Timestamp: ts, Bandwidth: value / fiveMinutes, Traffic: trafficCount}
		trendDatas = append(trendDatas, trandData)
	}

	return &pb.AllStatsResp{TrendDatas: trendDatas}, nil
}
