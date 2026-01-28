package logic

import (
	"context"
	"fmt"

	"titan-ipoverlay/ippop/rpc/serverapi"
	"titan-ipoverlay/manager/internal/svc"
	"titan-ipoverlay/manager/internal/types"

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

func (l *RemoveBlackListLogic) RemoveBlackList(req *types.RemoveBlacklistReq) (resp *types.UserOperationResp, err error) {
	if len(req.IPList) > maxIPListLen {
		return &types.UserOperationResp{ErrMsg: fmt.Sprintf("too many ips, max is %d", maxIPListLen)}, nil
	}

	server := l.svcCtx.Pops[string(req.PopID)]
	if server == nil {
		return &types.UserOperationResp{ErrMsg: fmt.Sprintf("pop %s not found", req.PopID)}, nil
	}

	removeBlacklistResp, err := server.API.RemoveBlacklist(l.ctx, &serverapi.RemoveBlacklistReq{IpList: req.IPList})
	if err != nil {
		return &types.UserOperationResp{ErrMsg: err.Error()}, nil
	}

	return &types.UserOperationResp{Success: removeBlacklistResp.Success, ErrMsg: removeBlacklistResp.ErrMsg}, nil
}
