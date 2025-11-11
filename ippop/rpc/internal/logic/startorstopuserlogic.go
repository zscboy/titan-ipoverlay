package logic

import (
	"context"
	"fmt"

	"titan-ipoverlay/ippop/model"
	"titan-ipoverlay/ippop/rpc/internal/svc"
	"titan-ipoverlay/ippop/rpc/pb"

	"github.com/zeromicro/go-zero/core/logx"
)

const (
	actionStart = "start"
	ActonStop   = "stop"
)

type StartOrStopUserLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewStartOrStopUserLogic(ctx context.Context, svcCtx *svc.ServiceContext) *StartOrStopUserLogic {
	return &StartOrStopUserLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *StartOrStopUserLogic) StartOrStopUser(in *pb.StartOrStopUserReq) (*pb.UserOperationResp, error) {
	if in.Action != actionStart && in.Action != ActonStop {
		return &pb.UserOperationResp{ErrMsg: "Actoin not start or stop"}, nil
	}

	user, err := model.GetUser(l.svcCtx.Redis, in.UserName)
	if err != nil {
		return &pb.UserOperationResp{ErrMsg: err.Error()}, nil
	}

	if user == nil {
		return &pb.UserOperationResp{ErrMsg: fmt.Sprintf("user %s not exist", in.UserName)}, nil
	}

	user.Off = (in.Action == ActonStop)

	err = model.SaveUser(l.svcCtx.Redis, user)
	if err != nil {
		return &pb.UserOperationResp{ErrMsg: err.Error()}, nil
	}

	if err := l.svcCtx.DeleteCache(in.UserName); err != nil {
		return &pb.UserOperationResp{ErrMsg: err.Error()}, nil
	}

	return &pb.UserOperationResp{Success: true}, nil
}
