package logic

import (
	"context"
	"fmt"

	"titan-ipoverlay/ippop/model"
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

	if err := checkRoute(l.ctx, l.svcCtx.Redis, in.Route); err != nil {
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

	oldRouteModel := user.RouteMode

	user.StartTime = in.TrafficLimit.StartTime
	user.EndTime = in.TrafficLimit.EndTime
	user.TotalTraffic = in.TrafficLimit.TotalTraffic
	user.RouteMode = int(in.Route.Mode)
	user.UpdateRouteIntervalMinutes = int(in.Route.IntervalMinutes)
	user.UpdateRouteUtcMinuteOfDay = int(in.Route.UtcMinuteOfDay)

	if err := model.SwitchNodeByUser(l.ctx, l.svcCtx.Redis, user, in.Route.NodeId); err != nil {
		return &pb.UserOperationResp{ErrMsg: err.Error()}, nil
	}

	// add user to scheduler list
	if oldRouteModel != int(model.RouteModeTimed) && user.RouteMode == int(model.RouteModeTimed) {
		if err := model.AddUserToSchedulerList(l.svcCtx.Redis, user.UserName); err != nil {
			return &pb.UserOperationResp{ErrMsg: err.Error()}, nil
		}
	}

	// remove user from scheduler list
	if oldRouteModel == int(model.RouteModeTimed) && user.RouteMode != int(model.RouteModeTimed) {
		if err := model.RemoveUserFromSchedulerList(l.svcCtx.Redis, user.UserName); err != nil {
			return &pb.UserOperationResp{ErrMsg: err.Error()}, nil
		}
	}

	if err := l.svcCtx.DeleteCache(in.UserName); err != nil {
		return &pb.UserOperationResp{ErrMsg: err.Error()}, nil
	}

	return &pb.UserOperationResp{Success: true}, nil
}
