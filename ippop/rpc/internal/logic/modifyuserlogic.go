package logic

import (
	"context"
	"fmt"

	"titan-ipoverlay/ippop/api/model"
	"titan-ipoverlay/ippop/rpc/internal/svc"
	"titan-ipoverlay/ippop/rpc/pb"

	"github.com/zeromicro/go-zero/core/logx"
)

type ModifyUserLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewModifyUserLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ModifyUserLogic {
	return &ModifyUserLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *ModifyUserLogic) ModifyUser(in *pb.ModifyUserReq) (*pb.UserOperationResp, error) {
	if in.TrafficLimit == nil {
		return nil, fmt.Errorf("traffic limit not allow")
	}

	if in.Route == nil {
		return nil, fmt.Errorf("route not allow")
	}

	if err := checkRoute(l.svcCtx.Redis, in.Route); err != nil {
		return nil, err
	}

	if err := checkTraffic(in.TrafficLimit); err != nil {
		return nil, err
	}

	user, err := model.GetUser(l.svcCtx.Redis, in.UserName)
	if err != nil {
		return &pb.UserOperationResp{ErrMsg: err.Error()}, nil
	}

	if user == nil {
		return &pb.UserOperationResp{ErrMsg: fmt.Sprintf("user %s not exist", in.UserName)}, nil
	}

	if user.RouteNodeID == in.Route.NodeId {
		return &pb.UserOperationResp{ErrMsg: fmt.Sprintf("user %s already bind node %s", in.UserName, user.RouteNodeID)}, nil
	}

	user.StartTime = in.TrafficLimit.StartTime
	user.EndTime = in.TrafficLimit.EndTime
	user.TotalTraffic = in.TrafficLimit.TotalTraffic
	user.RouteMode = int(in.Route.Mode)
	user.UpdateRouteIntervals = int(in.Route.Intervals)

	if err := model.SwitchNodeByUser(l.ctx, l.svcCtx.Redis, user, in.Route.NodeId); err != nil {
		return &pb.UserOperationResp{ErrMsg: err.Error()}, nil
	}

	deleteUserCacheLogic := NewDeleteUserCache(l.ctx, l.svcCtx)
	if err := deleteUserCacheLogic.DeleteUserCache(in.UserName); err != nil {

		return &pb.UserOperationResp{ErrMsg: err.Error()}, nil
	}

	return &pb.UserOperationResp{Success: true}, nil
}
