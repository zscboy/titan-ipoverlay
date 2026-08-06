package logic

import (
	"context"
	"fmt"

	"titan-ipoverlay/manager/internal/svc"
	"titan-ipoverlay/manager/internal/types"
	"titan-ipoverlay/manager/model"

	"github.com/zeromicro/go-zero/core/logx"
)

type AddIPBlacklistV2Logic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewAddIPBlacklistV2Logic(ctx context.Context, svcCtx *svc.ServiceContext) *AddIPBlacklistV2Logic {
	return &AddIPBlacklistV2Logic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *AddIPBlacklistV2Logic) AddIPBlacklistV2(req *types.IPBlacklistReq) (resp *types.UserOperationResp, err error) {
	if len(req.IPList) > maxIPListLen {
		return &types.UserOperationResp{ErrMsg: fmt.Sprintf("too many ips, max is %d", maxIPListLen)}, nil
	}

	logx.Infof("AddIPBlacklistV2Logic: AddIPBlacklistV2, ip_list len: %d", len(req.IPList))
	
	// Add to manager's global IP blacklist in Redis
	if err := model.AddIPBlacklist(l.svcCtx.Redis, req.IPList); err != nil {
		return &types.UserOperationResp{ErrMsg: err.Error()}, nil
	}

	// Store locally in manager's in-memory sync.Map
	for _, ip := range req.IPList {
		l.svcCtx.BlacklistMap.Store(ip, true)
	}

	return &types.UserOperationResp{Success: true}, nil
}
