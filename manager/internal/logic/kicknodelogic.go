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

type KickNodeLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewKickNodeLogic(ctx context.Context, svcCtx *svc.ServiceContext) *KickNodeLogic {
	return &KickNodeLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *KickNodeLogic) KickNode(req *types.KickNodeReq) (resp *types.UserOperationResp, err error) {
	popID, _, err := model.GetNodePopIP(l.svcCtx.Redis, req.NodeID)
	if err != nil {
		return &types.UserOperationResp{ErrMsg: err.Error()}, nil
	}

	if len(popID) == 0 {
		return &types.UserOperationResp{ErrMsg: fmt.Sprintf("node %s not exist", req.NodeID)}, nil
	}

	server := l.svcCtx.Servers[string(popID)]
	if server == nil {
		return &types.UserOperationResp{ErrMsg: fmt.Sprintf("pop %s not found", popID)}, nil
	}

	kickNodeResp, err := server.API.KickNode(l.ctx, &serverapi.KickNodeReq{NodeId: req.NodeID})
	if err != nil {
		return &types.UserOperationResp{ErrMsg: err.Error()}, nil
	}

	return &types.UserOperationResp{Success: kickNodeResp.Success, ErrMsg: kickNodeResp.ErrMsg}, nil
}
