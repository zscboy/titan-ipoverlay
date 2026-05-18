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

type GetUserBaseStatsLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewGetUserBaseStatsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetUserBaseStatsLogic {
	return &GetUserBaseStatsLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetUserBaseStatsLogic) GetUserBaseStats(req *types.UserBaseStatsReq) (resp *types.UserBaseStatsResp, err error) {
	popIDs, err := model.GetUserPops(l.svcCtx.Redis, req.Username)
	if err != nil {
		return nil, err
	}

	if len(popIDs) == 0 {
		return nil, fmt.Errorf("user %s not exist", req.Username)
	}

	var currentBandwidth, topBandwidth, totalTraffic int64
	for _, popID := range popIDs {
		server := l.svcCtx.Pops[popID]
		if server == nil {
			continue
		}
		baseStatsResp, err := server.API.GetUserBaseStats(l.ctx, &serverapi.UserBaseStatsReq{Username: req.Username})
		if err != nil {
			return nil, err
		}
		currentBandwidth += baseStatsResp.CurrentBandwidth
		topBandwidth += baseStatsResp.TopBandwidth
		totalTraffic += baseStatsResp.TotalTraffic
	}

	return &types.UserBaseStatsResp{
		CurrentBandwidth: currentBandwidth,
		TopBandwidth:     topBandwidth,
		TotalTraffic:     totalTraffic,
		CurrentConns:     0,
	}, nil
}
