package logic

import (
	"context"
	"fmt"
	"strings"

	"titan-ipoverlay/ippop/rpc/serverapi"
	"titan-ipoverlay/manager/internal/svc"
	"titan-ipoverlay/manager/internal/types"
	"titan-ipoverlay/manager/model"

	"github.com/zeromicro/go-zero/core/logx"
)

type GetUserLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewGetUserLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetUserLogic {
	return &GetUserLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetUserLogic) GetUser(req *types.GetUserReq) (resp *types.GetUserResp, err error) {
	popIDs, err := model.GetUserPops(l.svcCtx.Redis, req.UserName)
	if err != nil {
		return nil, err
	}

	if len(popIDs) == 0 {
		return nil, fmt.Errorf("user %s not exist", req.UserName)
	}

	server := l.svcCtx.Pops[popIDs[0]]
	if server == nil {
		return nil, fmt.Errorf("pop %s not found", popIDs[0])
	}

	getUserResp, err := server.API.GetUser(l.ctx, &serverapi.GetUserReq{UserName: req.UserName})
	if err != nil {
		return nil, err
	}

	traffic := toTrafficLimitResp(getUserResp.TrafficLimit)
	route := toRouteResp(getUserResp.Route)

	user := types.User{
		UserName:            getUserResp.UserName,
		NodeIP:              getUserResp.NodeIp,
		NodeOnline:          getUserResp.NodeOnline,
		CurrentTraffic:      getUserResp.CurrentTraffic,
		Off:                 getUserResp.Off,
		TrafficLimit:        traffic,
		Route:               route,
		UploadRateLimit:     getUserResp.UploadRateLimite,
		DownloadRateLimit:   getUserResp.DownloadRateLimit,
		LastRouteSwitchTime: getUserResp.LastRouteSwitchTime,
	}
	return &types.GetUserResp{
		PopId:  strings.Join(popIDs, ","),
		PopIds: popIDs,
		User:   user,
	}, nil
}
