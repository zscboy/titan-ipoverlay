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

type StartOrStopUserLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewStartOrStopUserLogic(ctx context.Context, svcCtx *svc.ServiceContext) *StartOrStopUserLogic {
	return &StartOrStopUserLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *StartOrStopUserLogic) StartOrStopUser(req *types.StartOrStopUserReq) (resp *types.UserOperationResp, err error) {
	popIDs, err := model.GetUserPops(l.svcCtx.Redis, req.UserName)
	if err != nil {
		return &types.UserOperationResp{ErrMsg: err.Error()}, nil
	}

	if len(popIDs) == 0 {
		return &types.UserOperationResp{ErrMsg: fmt.Sprintf("user %s not exist", req.UserName)}, nil
	}

	var errMsg string
	var success = true
	for _, popID := range popIDs {
		server := l.svcCtx.Pops[popID]
		if server == nil {
			logx.Errorf("pop %s not found for user %s", popID, req.UserName)
			continue
		}

		in := &serverapi.StartOrStopUserReq{UserName: req.UserName, Action: req.Action}
		startOrStopResp, err := server.API.StartOrStopUser(l.ctx, in)
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
