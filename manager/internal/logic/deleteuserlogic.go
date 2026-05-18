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

type DeleteUserLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewDeleteUserLogic(ctx context.Context, svcCtx *svc.ServiceContext) *DeleteUserLogic {
	return &DeleteUserLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *DeleteUserLogic) DeleteUser(req *types.DeleteUserReq) (resp *types.UserOperationResp, err error) {
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

		deleteUserResp, err := server.API.DeleteUser(l.ctx, &serverapi.DeleteUserReq{UserName: req.UserName})
		if err != nil {
			success = false
			errMsg += fmt.Sprintf("pop %s: %s; ", popID, err.Error())
		} else if !deleteUserResp.Success {
			success = false
			errMsg += fmt.Sprintf("pop %s: %s; ", popID, deleteUserResp.ErrMsg)
		}
	}

	if err := model.DeleteUser(l.svcCtx.Redis, req.UserName); err != nil {
		return &types.UserOperationResp{ErrMsg: err.Error()}, nil
	}

	return &types.UserOperationResp{Success: success, ErrMsg: errMsg}, nil
}
