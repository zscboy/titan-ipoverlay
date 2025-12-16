package logic

import (
	"context"
	"fmt"

	"titan-ipoverlay/ippop/rpc/serverapi"
	"titan-ipoverlay/manager/internal/logic/util"
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
	server := l.svcCtx.Pops[req.PopId]
	if server == nil {
		return nil, fmt.Errorf("pop %s not found", req.PopId)
	}

	popID, err := model.GetUserPop(l.svcCtx.Redis, req.UserName)
	if err != nil {
		return nil, fmt.Errorf("get user %s failed: %v", req.UserName, err)
	}

	if len(popID) != 0 {
		return nil, fmt.Errorf("user %s already exist", req.UserName)
	}

	in := &serverapi.CreateUserReq{UserName: req.UserName, Password: req.Password, UploadRateLimite: req.UploadRateLimit, DownloadRateLimit: req.DownloadRateLimit}
	if req.TrafficLimit != nil {
		in.TrafficLimit = util.ToTrafficLimitReq(req.TrafficLimit)
	}

	if req.Route != nil {
		in.Route = util.ToRouteReq(req.Route)
	}

	createUserResp, err := server.API.CreateUser(l.ctx, in)
	if err != nil {
		return nil, err
	}

	if err := model.SetUserPop(l.svcCtx.Redis, req.UserName, req.PopId); err != nil {
		return nil, err
	}

	return util.ToCreateUserResp(createUserResp), nil
}
