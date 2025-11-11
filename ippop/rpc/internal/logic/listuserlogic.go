package logic

import (
	"context"

	"titan-ipoverlay/ippop/model"
	"titan-ipoverlay/ippop/rpc/internal/svc"
	"titan-ipoverlay/ippop/rpc/pb"

	"github.com/zeromicro/go-zero/core/logx"
)

type ListUserLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewListUserLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ListUserLogic {
	return &ListUserLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *ListUserLogic) ListUser(in *pb.ListUserReq) (*pb.ListUserResp, error) {
	users, err := model.ListUser(l.ctx, l.svcCtx.Redis, int(in.Start), int(in.End))
	if err != nil {
		return nil, err
	}

	nodeIDs := make([]string, 0, len(users))
	us := make([]*pb.User, 0, len(users))
	for _, user := range users {
		trafficLimit := pb.TrafficLimit{StartTime: user.StartTime, EndTime: user.EndTime, TotalTraffic: user.TotalTraffic}
		route := pb.Route{Mode: int32(user.RouteMode), NodeId: user.RouteNodeID, IntervalMinutes: int32(user.UpdateRouteIntervalMinutes), UtcMinuteOfDay: int32(user.UpdateRouteUtcMinuteOfDay)}
		u := &pb.User{
			UserName:     user.UserName,
			TrafficLimit: &trafficLimit,
			Route:        &route, CurrentTraffic: user.CurrentTraffic,
			Off:                 user.Off,
			DownloadRateLimit:   user.DownloadRateLimit,
			UploadRateLimite:    user.UploadRateLimit,
			LastRouteSwitchTime: user.LastRouteSwitchTime,
		}
		us = append(us, u)

		if len(user.RouteNodeID) > 0 {
			nodeIDs = append(nodeIDs, user.RouteNodeID)
		}
	}

	nodes, err := model.ListNodeWithIDs(l.ctx, l.svcCtx.Redis, nodeIDs)
	if err != nil {
		return nil, err
	}

	nodeMap := make(map[string]*model.Node)
	for _, node := range nodes {
		nodeMap[node.Id] = node
	}

	for _, user := range us {
		if user.Route == nil {
			continue
		}

		node := nodeMap[user.Route.NodeId]
		if node == nil {
			continue
		}

		user.NodeIp = node.IP
		user.NodeOnline = node.Online
	}

	total, err := model.GetUserLen(l.svcCtx.Redis)
	if err != nil {
		return nil, err
	}

	return &pb.ListUserResp{Users: us, Total: int32(total)}, nil
}
