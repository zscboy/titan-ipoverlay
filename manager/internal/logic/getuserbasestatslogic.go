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
	popID, err := model.GetUserPop(l.svcCtx.Redis, req.Username)
	if err != nil {
		return nil, err
	}

	if len(popID) == 0 {
		return nil, fmt.Errorf("user %s not exist", req.Username)
	}

	server := l.svcCtx.Pops[popID]
	if server == nil {
		return nil, fmt.Errorf("pop %s not found", popID)
	}

	baseStatsResp, err := server.API.GetUserBaseStats(l.ctx, &serverapi.UserBaseStatsReq{Username: req.Username})
	if err != nil {
		return nil, err
	}
	return &types.UserBaseStatsResp{
		CurrentBandwidth: baseStatsResp.CurrentBandwidth,
		TopBandwidth:     baseStatsResp.TopBandwidth,
		TotalTraffic:     baseStatsResp.TotalTraffic,
		CurrentConns:     0,
	}, nil
}
