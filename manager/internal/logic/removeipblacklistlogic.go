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

type RemoveIPBlacklistLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewRemoveIPBlacklistLogic(ctx context.Context, svcCtx *svc.ServiceContext) *RemoveIPBlacklistLogic {
	return &RemoveIPBlacklistLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *RemoveIPBlacklistLogic) RemoveIPBlacklist(req *types.IPBlacklistReq) (resp *types.UserOperationResp, err error) {
	if len(req.IPList) > maxIPListLen {
		return &types.UserOperationResp{ErrMsg: fmt.Sprintf("too many ips, max is %d", maxIPListLen)}, nil
	}

	// 1. Remove from manager's global IP blacklist
	if err := model.RemoveIPBlacklist(l.svcCtx.Redis, req.IPList); err != nil {
		return &types.UserOperationResp{ErrMsg: err.Error()}, nil
	}

	// 2. Use the configured BlacklistPop to kick nodes
	// These nodes are currently connected to the BlacklistPop. By kicking them now,
	// when they reconnect, the manager will re-allocate them to a normal POP because they're no longer blacklisted.
	blacklistPopID := l.svcCtx.Config.BlacklistPop
	if blacklistPopID != "" {
		server := l.svcCtx.Pops[blacklistPopID]
		if server == nil {
			logx.Errorf("configured blacklist pop %s not found", blacklistPopID)
		} else {
			_, err := server.API.KickNodeByIP(l.ctx, &serverapi.KickNodeByIPReq{
				IpList: req.IPList,
			})
			if err != nil {
				logx.Errorf("RemoveIPBlacklist: KickNodeByIP failed for blacklist pop %s: %v", blacklistPopID, err)
				// We don't return error here because the DB removal was successful,
				// but we log it for troubleshooting.
			} else {
				logx.Infof("Successfully kicked %d revoked IPs from blacklist pop %s", len(req.IPList), blacklistPopID)
			}
		}
	} else {
		logx.Errorf("BlacklistPop ID not configured, nodes won't be kicked automatically")
	}

	return &types.UserOperationResp{Success: true}, nil
}
