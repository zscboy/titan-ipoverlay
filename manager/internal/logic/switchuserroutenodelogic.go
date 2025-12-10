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

type SwitchUserRouteNodeLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewSwitchUserRouteNodeLogic(ctx context.Context, svcCtx *svc.ServiceContext) *SwitchUserRouteNodeLogic {
	return &SwitchUserRouteNodeLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *SwitchUserRouteNodeLogic) SwitchUserRouteNode(req *types.SwitchUserRouteNodeReq) (resp *types.UserOperationResp, err error) {
	popID, err := model.GetUserPop(l.svcCtx.Redis, req.UserName)
	if err != nil {
		return &types.UserOperationResp{ErrMsg: err.Error()}, nil
	}

	if len(popID) == 0 {
		return &types.UserOperationResp{ErrMsg: fmt.Sprintf("user %s not exist", req.UserName)}, nil
	}

	server := l.svcCtx.Pops[popID]
	if server == nil {
		return &types.UserOperationResp{ErrMsg: fmt.Sprintf("pop %s not found", popID)}, nil
	}

	in := &serverapi.SwitchUserRouteNodeReq{UserName: req.UserName, NodeId: req.NodeId}
	startOrStopResp, err := server.API.SwitchUserRouteNode(l.ctx, in)
	if err != nil {
		return &types.UserOperationResp{ErrMsg: err.Error()}, nil
	}

	return &types.UserOperationResp{Success: startOrStopResp.Success, ErrMsg: startOrStopResp.ErrMsg}, nil
}
