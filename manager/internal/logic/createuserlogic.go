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

type CreateUserLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewCreateUserLogic(ctx context.Context, svcCtx *svc.ServiceContext) *CreateUserLogic {
	return &CreateUserLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *CreateUserLogic) CreateUser(req *types.CreateUserReq) (resp *types.CreateUserResp, err error) {
	if len(req.PopIds) == 0 {
		return nil, fmt.Errorf("pop_ids cannot be empty")
	}

	// 1. Validate all request pop_ids exist in svcCtx.Pops
	for _, popID := range req.PopIds {
		if l.svcCtx.Pops[popID] == nil {
			return nil, fmt.Errorf("pop %s not found", popID)
		}
	}

	// 2. Retrieve already registered POPs for the user
	existingPopIDs, err := model.GetUserPops(l.svcCtx.Redis, req.UserName)
	if err != nil {
		return nil, fmt.Errorf("get user %s failed: %v", req.UserName, err)
	}

	// 3. If any pop in the request is already registered for this user, return error
	var alreadyExists []string
	for _, reqPopID := range req.PopIds {
		for _, existPopID := range existingPopIDs {
			if reqPopID == existPopID {
				alreadyExists = append(alreadyExists, reqPopID)
			}
		}
	}
	if len(alreadyExists) > 0 {
		return nil, fmt.Errorf("user %s already exists on pops: %v", req.UserName, alreadyExists)
	}

	// 4. Create user in all requested POPs, collecting success and failed lists
	var firstCreateUserResp *serverapi.CreateUserResp
	var successPops []string
	var failedPops []string

	for _, popID := range req.PopIds {
		server := l.svcCtx.Pops[popID]
		in := &serverapi.CreateUserReq{UserName: req.UserName, Password: req.Password, UploadRateLimite: req.UploadRateLimit, DownloadRateLimit: req.DownloadRateLimit}
		if req.TrafficLimit != nil {
			in.TrafficLimit = toTrafficLimitReq(req.TrafficLimit)
		}

		if req.Route != nil {
			in.Route = toRouteReq(req.Route)
		}

		createUserResp, err := server.API.CreateUser(l.ctx, in)
		if err != nil {
			logx.Errorf("failed to create user on pop %s: %v", popID, err)
			failedPops = append(failedPops, popID)
			continue
		}

		successPops = append(successPops, popID)
		if firstCreateUserResp == nil {
			firstCreateUserResp = createUserResp
		}
	}

	// 5. If all POPs failed, return an error
	if len(successPops) == 0 {
		return nil, fmt.Errorf("failed to create user on all POPs: %v", failedPops)
	}

	// 6. Save successfully created user pop mappings to Redis in bulk
	if err := model.SetUserPop(l.svcCtx.Redis, req.UserName, successPops); err != nil {
		return nil, fmt.Errorf("failed to save user pop mapping to Redis: %v", err)
	}

	// 7. Build response using the first successful response metadata
	ret := toCreateUserResp(firstCreateUserResp)
	ret.SuccessPops = successPops
	ret.FailedPops = failedPops
	return ret, nil
}
