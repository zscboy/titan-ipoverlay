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
	popIDs, err := model.GetUserPops(l.svcCtx.Redis, req.UserName)
	if err != nil {
		return &types.UserOperationResp{ErrMsg: err.Error()}, nil
	}

	if len(popIDs) == 0 {
		return &types.UserOperationResp{ErrMsg: fmt.Sprintf("user %s not exist", req.UserName)}, nil
	}

	// Determine which POP the NodeId belongs to
	var targetPopID string
	if len(req.NodeId) > 0 {
		popBytes, _, err := model.GetNodePopIP(l.svcCtx.Redis, req.NodeId)
		if err != nil {
			return &types.UserOperationResp{ErrMsg: fmt.Sprintf("get node pop failed: %v", err)}, nil
		}
		if len(popBytes) > 0 {
			targetPopID = string(popBytes)
		}
	}

	// If a target POP is found and the user is registered on it, call only that POP's API.
	// Otherwise, if NodeId is empty, clear route node on all POPs the user belongs to.
	if len(targetPopID) > 0 {
		// Verify if the user belongs to this target POP
		userBelongs := false
		for _, p := range popIDs {
			if p == targetPopID {
				userBelongs = true
				break
			}
		}
		if !userBelongs {
			return &types.UserOperationResp{ErrMsg: fmt.Sprintf("user %s does not belong to pop %s", req.UserName, targetPopID)}, nil
		}

		server := l.svcCtx.Pops[targetPopID]
		if server == nil {
			return &types.UserOperationResp{ErrMsg: fmt.Sprintf("pop %s not found", targetPopID)}, nil
		}
		in := &serverapi.SwitchUserRouteNodeReq{UserName: req.UserName, NodeId: req.NodeId}
		startOrStopResp, err := server.API.SwitchUserRouteNode(l.ctx, in)
		if err != nil {
			return &types.UserOperationResp{ErrMsg: err.Error()}, nil
		}
		return &types.UserOperationResp{Success: startOrStopResp.Success, ErrMsg: startOrStopResp.ErrMsg}, nil
	} else {
		// If NodeId is empty, clear on all user's POPs
		var errMsg string
		var success = true
		for _, popID := range popIDs {
			server := l.svcCtx.Pops[popID]
			if server == nil {
				continue
			}
			in := &serverapi.SwitchUserRouteNodeReq{UserName: req.UserName, NodeId: req.NodeId}
			startOrStopResp, err := server.API.SwitchUserRouteNode(l.ctx, in)
			if err != nil {
				success = false
				errMsg += fmt.Sprintf("pop %s: %s; ", popID, err.Error())
			} else if !startOrStopResp.Success {
				success = false
				errMsg += fmt.Sprintf("pop %s: %s; ", popID, startOrStopResp.ErrMsg)
			}
		}
		return &types.UserOperationResp{Success: success, ErrMsg: errMsg}, nil
	}
}
