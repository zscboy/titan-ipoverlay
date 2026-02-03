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

type AddIPBlacklistLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewAddIPBlacklistLogic(ctx context.Context, svcCtx *svc.ServiceContext) *AddIPBlacklistLogic {
	return &AddIPBlacklistLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *AddIPBlacklistLogic) AddIPBlacklist(req *types.IPBlacklistReq) (resp *types.UserOperationResp, err error) {
	if len(req.IPList) > maxIPListLen {
		return &types.UserOperationResp{ErrMsg: fmt.Sprintf("too many ips, max is %d", maxIPListLen)}, nil
	}

	// 1. Validate PopID is provided
	if req.PopID == "" {
		return &types.UserOperationResp{ErrMsg: "pop_id is required"}, nil
	}

	// 2. Add to manager's global IP blacklist
	if err := model.AddIPBlacklist(l.svcCtx.Redis, req.IPList); err != nil {
		return &types.UserOperationResp{ErrMsg: err.Error()}, nil
	}

	// 3. Kick nodes from the specified POP immediately
	server := l.svcCtx.Pops[req.PopID]
	if server == nil {
		return &types.UserOperationResp{ErrMsg: fmt.Sprintf("pop %s not found", req.PopID)}, nil
	}

	_, err = server.API.KickNodeByIP(l.ctx, &serverapi.KickNodeByIPReq{
		IpList: req.IPList,
	})
	if err != nil {
		logx.Errorf("KickNodeByIP failed for pop %s: %v", req.PopID, err)
		return &types.UserOperationResp{ErrMsg: fmt.Sprintf("Add blacklist success but kick node failed: %v", err)}, nil
	}

	return &types.UserOperationResp{Success: true}, nil
}
