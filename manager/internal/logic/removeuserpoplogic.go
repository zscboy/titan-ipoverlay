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

type RemoveUserPopLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewRemoveUserPopLogic(ctx context.Context, svcCtx *svc.ServiceContext) *RemoveUserPopLogic {
	return &RemoveUserPopLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *RemoveUserPopLogic) RemoveUserPop(req *types.RemoveUserPopReq) (resp *types.UserOperationResp, err error) {
	if len(req.PopIds) == 0 {
		return &types.UserOperationResp{Success: false, ErrMsg: "pop_ids cannot be empty"}, nil
	}

	// 1. Validate requested POPs exist in svcCtx.Pops
	for _, popID := range req.PopIds {
		if l.svcCtx.Pops[popID] == nil {
			return &types.UserOperationResp{Success: false, ErrMsg: fmt.Sprintf("pop %s not found", popID)}, nil
		}
	}

	// 2. Retrieve user's existing POPs
	existingPopIDs, err := model.GetUserPops(l.svcCtx.Redis, req.UserName)
	if err != nil {
		return &types.UserOperationResp{Success: false, ErrMsg: err.Error()}, nil
	}

	if len(existingPopIDs) == 0 {
		return &types.UserOperationResp{Success: false, ErrMsg: fmt.Sprintf("user %s does not exist", req.UserName)}, nil
	}

	// 3. Filter out POPs that are actually bound
	existingMap := make(map[string]bool)
	for _, pid := range existingPopIDs {
		existingMap[pid] = true
	}

	var popsToRemove []string
	for _, pid := range req.PopIds {
		if existingMap[pid] {
			popsToRemove = append(popsToRemove, pid)
		}
	}

	// If no requested POPs are bound to the user, return success immediately
	if len(popsToRemove) == 0 {
		return &types.UserOperationResp{Success: true}, nil
	}

	// 4. Delete user on the specified POP servers
	var successRemovals []string
	var failedRemovals []string
	var errMsg string

	for _, popID := range popsToRemove {
		server := l.svcCtx.Pops[popID]
		deleteUserResp, err := server.API.DeleteUser(l.ctx, &serverapi.DeleteUserReq{UserName: req.UserName})
		if err != nil {
			logx.Errorf("failed to delete user %s on pop %s: %v", req.UserName, popID, err)
			failedRemovals = append(failedRemovals, popID)
			errMsg += fmt.Sprintf("pop %s: %v; ", popID, err)
		} else if !deleteUserResp.Success {
			logx.Errorf("failed to delete user %s on pop %s: %s", req.UserName, popID, deleteUserResp.ErrMsg)
			failedRemovals = append(failedRemovals, popID)
			errMsg += fmt.Sprintf("pop %s: %s; ", popID, deleteUserResp.ErrMsg)
		} else {
			successRemovals = append(successRemovals, popID)
		}
	}

	// 5. If all requested POP removals failed, return failure
	if len(successRemovals) == 0 {
		return &types.UserOperationResp{Success: false, ErrMsg: fmt.Sprintf("failed to remove on all POPs: %s", errMsg)}, nil
	}

	// 6. Update user POP mappings in Redis (remove successful POPs)
	if err := model.RemoveUserPops(l.svcCtx.Redis, req.UserName, successRemovals); err != nil {
		return &types.UserOperationResp{Success: false, ErrMsg: fmt.Sprintf("failed to update user pop mapping in Redis: %v", err)}, nil
	}

	if len(failedRemovals) > 0 {
		return &types.UserOperationResp{Success: false, ErrMsg: fmt.Sprintf("partial success. failed on pops: %v. error: %s", failedRemovals, errMsg)}, nil
	}

	return &types.UserOperationResp{Success: true}, nil
}
