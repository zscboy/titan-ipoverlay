package logic

import (
	"context"

	"titan-ipoverlay/ippop/rpc/internal/svc"
	"titan-ipoverlay/ippop/rpc/pb"

	"github.com/zeromicro/go-zero/core/logx"
)

type RemoveBlacklistLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewRemoveBlacklistLogic(ctx context.Context, svcCtx *svc.ServiceContext) *RemoveBlacklistLogic {
	return &RemoveBlacklistLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *RemoveBlacklistLogic) RemoveBlacklist(in *pb.RemoveBlacklistReq) (*pb.UserOperationResp, error) {
	if len(in.IpList) > maxIPListLen {
		return &pb.UserOperationResp{ErrMsg: "too many ips"}, nil
	}
	if err := l.svcCtx.RemoveBlacklist(in.IpList); err != nil {
		return &pb.UserOperationResp{ErrMsg: err.Error()}, nil
	}

	return &pb.UserOperationResp{Success: true}, nil
}
