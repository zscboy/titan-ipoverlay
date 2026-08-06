package logic

import (
	"context"
	"fmt"
	"time"

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

	logx.Infof("AddIPBlacklistLogic: AddIPBlacklist, pop_id: %s, ip_list len: %d", req.PopID, len(req.IPList))
	// 2. Add to manager's global IP blacklist
	if err := model.AddIPBlacklist(l.svcCtx.Redis, req.IPList); err != nil {
		return &types.UserOperationResp{ErrMsg: err.Error()}, nil
	}

	for _, ip := range req.IPList {
		l.svcCtx.BlacklistMap.Store(ip, true)
	}

	// 3. Kick nodes from the specified POP immediately
	server := l.svcCtx.Pops[req.PopID]
	if server == nil {
		return &types.UserOperationResp{ErrMsg: fmt.Sprintf("pop %s not found", req.PopID)}, nil
	}

	startTime := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	_, err = server.API.KickNodeByIP(ctx, &serverapi.KickNodeByIPReq{
		IpList: req.IPList,
	})
	if err != nil {
		logx.Errorf("KickNodeByIP failed for pop %s: %v, kick ips cost time:%v", req.PopID, err, time.Since(startTime))
		return &types.UserOperationResp{ErrMsg: fmt.Sprintf("Add blacklist success but kick node failed: %v,  kick ips cost time:%v", err, time.Since(startTime))}, nil
	}

	return &types.UserOperationResp{Success: true}, nil
}
