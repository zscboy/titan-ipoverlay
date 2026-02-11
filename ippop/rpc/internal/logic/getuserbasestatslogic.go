package logic

import (
	"context"
	"time"

	"titan-ipoverlay/ippop/model"
	"titan-ipoverlay/ippop/rpc/internal/svc"
	"titan-ipoverlay/ippop/rpc/pb"

	"github.com/zeromicro/go-zero/core/logx"
)

type GetUserBaseStatsLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
	fiveMinute int64
}

func NewGetUserBaseStatsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetUserBaseStatsLogic {
	return &GetUserBaseStatsLogic{
		ctx:        ctx,
		svcCtx:     svcCtx,
		Logger:     logx.WithContext(ctx),
		fiveMinute: 5 * 60,
	}
}

func (l *GetUserBaseStatsLogic) GetUserBaseStats(in *pb.UserBaseStatsReq) (*pb.UserBaseStatsResp, error) {
	timestamp, traffic, err := model.GetUserLastTrafficPer5Min(l.svcCtx.Redis, in.Username)
	if err != nil {
		return nil, err
	}
	logx.Debugf("timestamp:%d, traffic:%d", timestamp, traffic)
	currentBandwidth := int64(0)
	if timestamp > (time.Now().Unix() - l.fiveMinute) {
		currentBandwidth = traffic / l.fiveMinute
	}

	user, err := model.GetUser(l.svcCtx.Redis, in.Username)
	if err != nil {
		return nil, err
	}

	if user == nil {
		return &pb.UserBaseStatsResp{CurrentBandwidth: currentBandwidth}, nil
	}

	// TODO: Add connection count
	return &pb.UserBaseStatsResp{CurrentBandwidth: currentBandwidth, TotalTraffic: user.CurrentTraffic, TopBandwidth: user.TopBandwidth}, nil
}
