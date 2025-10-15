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

type RemoveBlackListLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewRemoveBlackListLogic(ctx context.Context, svcCtx *svc.ServiceContext) *RemoveBlackListLogic {
	return &RemoveBlackListLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *RemoveBlackListLogic) RemoveBlackList(req *types.RemoveBlackListReq) (resp *types.UserOperationResp, err error) {
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

	removeBlacklistResp, err := server.API.RemoveBlacklist(l.ctx, &serverapi.RemoveBlacklistReq{NodeId: req.NodeID})
	if err != nil {
		return &types.UserOperationResp{ErrMsg: err.Error()}, nil
	}

	return &types.UserOperationResp{Success: removeBlacklistResp.Success, ErrMsg: removeBlacklistResp.ErrMsg}, nil
}
