package logic

import (
	"context"
	"fmt"

	"titan-ipoverlay/ippop/api/model"
	"titan-ipoverlay/ippop/rpc/internal/svc"
	"titan-ipoverlay/ippop/rpc/pb"

	"github.com/zeromicro/go-zero/core/logx"
)

type DeleteUserLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewDeleteUserLogic(ctx context.Context, svcCtx *svc.ServiceContext) *DeleteUserLogic {
	return &DeleteUserLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *DeleteUserLogic) DeleteUser(in *pb.DeleteUserReq) (*pb.UserOperationResp, error) {
	user, err := model.GetUser(l.svcCtx.Redis, in.UserName)
	if err != nil {
		return &pb.UserOperationResp{ErrMsg: err.Error()}, nil
	}

	if user == nil {
		return &pb.UserOperationResp{ErrMsg: fmt.Sprintf("user %s not exist", in.UserName)}, nil
	}

	err = model.DeleteUser(l.svcCtx.Redis, in.UserName)
	if err != nil {
		return &pb.UserOperationResp{ErrMsg: err.Error()}, nil
	}

	if user.RouteMode == int(model.RouteModeTimed) {
		if err := model.RemoveUserFromSchedulerList(l.svcCtx.Redis, in.UserName); err != nil {
			return &pb.UserOperationResp{ErrMsg: err.Error()}, nil
		}
	}

	if len(user.RouteNodeID) > 0 {
		if err := model.UnbindNode(l.ctx, l.svcCtx.Redis, user.RouteNodeID); err != nil {
			return &pb.UserOperationResp{ErrMsg: err.Error()}, nil
		}
	}

	deleteUserCacheLogic := NewDeleteUserCache(l.ctx, l.svcCtx)
	if err := deleteUserCacheLogic.DeleteUserCache(in.UserName); err != nil {

		return &pb.UserOperationResp{ErrMsg: err.Error()}, nil
	}

	return &pb.UserOperationResp{Success: true}, nil
}
