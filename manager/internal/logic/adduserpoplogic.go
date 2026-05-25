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

type AddUserPopLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewAddUserPopLogic(ctx context.Context, svcCtx *svc.ServiceContext) *AddUserPopLogic {
	return &AddUserPopLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *AddUserPopLogic) AddUserPop(req *types.AddUserPopReq) (resp *types.UserOperationResp, err error) {
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

	// 3. Filter out POPs that are already bound
	existingMap := make(map[string]bool)
	for _, pid := range existingPopIDs {
		existingMap[pid] = true
	}

	var popsToBind []string
	for _, pid := range req.PopIds {
		if !existingMap[pid] {
			popsToBind = append(popsToBind, pid)
		}
	}

	// If all requested POPs are already bound, return success immediately
	if len(popsToBind) == 0 {
		return &types.UserOperationResp{Success: true}, nil
	}

	// 4. Fetch existing user config from the first registered POP server
	primaryPopID := existingPopIDs[0]
	primaryServer := l.svcCtx.Pops[primaryPopID]
	if primaryServer == nil {
		return &types.UserOperationResp{Success: false, ErrMsg: fmt.Sprintf("primary pop %s not found", primaryPopID)}, nil
	}

	getUserResp, err := primaryServer.API.GetUser(l.ctx, &serverapi.GetUserReq{UserName: req.UserName})
	if err != nil {
		return &types.UserOperationResp{Success: false, ErrMsg: fmt.Sprintf("fetch user from primary POP failed: %v", err)}, nil
	}

	// 5. Replicate/Create the user on the new POP servers
	var successPops []string
	var failedPops []string
	var errMsg string

	for _, popID := range popsToBind {
		newServer := l.svcCtx.Pops[popID]
		createUserReq := &serverapi.CreateUserReq{
			UserName:          req.UserName,
			PasswordMd5:       getUserResp.PasswordMd5,
			TrafficLimit:      getUserResp.TrafficLimit,
			Route:             getUserResp.Route,
			UploadRateLimite:  getUserResp.UploadRateLimite,
			DownloadRateLimit: getUserResp.DownloadRateLimit,
		}

		_, err = newServer.API.CreateUser(l.ctx, createUserReq)
		if err != nil {
			logx.Errorf("failed to replicate user %s on pop %s: %v", req.UserName, popID, err)
			failedPops = append(failedPops, popID)
			errMsg += fmt.Sprintf("pop %s: %v; ", popID, err)
		} else {
			successPops = append(successPops, popID)
		}
	}

	// 6. If all requested POPs failed to bind, return failure
	if len(successPops) == 0 {
		return &types.UserOperationResp{Success: false, ErrMsg: fmt.Sprintf("failed to bind on all POPs: %s", errMsg)}, nil
	}

	// 7. Update successfully bound user POP mappings in manager's Redis
	if err := model.SetUserPop(l.svcCtx.Redis, req.UserName, successPops); err != nil {
		return &types.UserOperationResp{Success: false, ErrMsg: fmt.Sprintf("failed to update user pop mapping in Redis: %v", err)}, nil
	}

	if len(failedPops) > 0 {
		return &types.UserOperationResp{Success: false, ErrMsg: fmt.Sprintf("partial success. failed on pops: %v. error: %s", failedPops, errMsg)}, nil
	}

	return &types.UserOperationResp{Success: true}, nil
}
