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

type AddBlackListLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewAddBlackListLogic(ctx context.Context, svcCtx *svc.ServiceContext) *AddBlackListLogic {
	return &AddBlackListLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *AddBlackListLogic) AddBlackList(req *types.AddBlackListReq) (resp *types.UserOperationResp, err error) {
	popID, _, err := model.GetNodePopIP(l.svcCtx.Redis, req.NodeID)
	if err != nil {
		return &types.UserOperationResp{ErrMsg: err.Error()}, nil
	}

	if len(popID) == 0 {
		return &types.UserOperationResp{ErrMsg: fmt.Sprintf("node %s not exist", req.NodeID)}, nil
	}

	server := l.svcCtx.Pops[string(popID)]
	if server == nil {
		return &types.UserOperationResp{ErrMsg: fmt.Sprintf("pop %s not found", popID)}, nil
	}

	addBlacklistResp, err := server.API.AddBlacklist(l.ctx, &serverapi.AddBlacklistReq{NodeId: req.NodeID})
	if err != nil {
		return &types.UserOperationResp{ErrMsg: err.Error()}, nil
	}

	return &types.UserOperationResp{Success: addBlacklistResp.Success, ErrMsg: addBlacklistResp.ErrMsg}, nil
}
