package logic

import (
	"context"
	"fmt"

	"titan-ipoverlay/ippop/rpc/serverapi"
	"titan-ipoverlay/manager/internal/svc"
	"titan-ipoverlay/manager/internal/types"

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

const maxIPListLen = 100

func (l *AddBlackListLogic) AddBlackList(req *types.AddBlacklistReq) (resp *types.UserOperationResp, err error) {
	if len(req.IPList) > maxIPListLen {
		return &types.UserOperationResp{ErrMsg: fmt.Sprintf("too many ips, max is %d", maxIPListLen)}, nil
	}

	server := l.svcCtx.Pops[req.PopID]
	if server == nil {
		return &types.UserOperationResp{ErrMsg: fmt.Sprintf("pop %s not found", req.PopID)}, nil
	}

	addBlacklistResp, err := server.API.AddBlacklist(l.ctx, &serverapi.AddBlacklistReq{IpList: req.IPList})
	if err != nil {
		return &types.UserOperationResp{ErrMsg: err.Error()}, nil
	}

	return &types.UserOperationResp{Success: addBlacklistResp.Success, ErrMsg: addBlacklistResp.ErrMsg}, nil
}
