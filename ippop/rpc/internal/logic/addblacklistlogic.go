package logic

import (
	"context"

	"titan-ipoverlay/ippop/rpc/internal/svc"
	"titan-ipoverlay/ippop/rpc/pb"

	"github.com/zeromicro/go-zero/core/logx"
)

type AddBlacklistLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewAddBlacklistLogic(ctx context.Context, svcCtx *svc.ServiceContext) *AddBlacklistLogic {
	return &AddBlacklistLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

const maxIPListLen = 100

func (l *AddBlacklistLogic) AddBlacklist(req *pb.AddBlacklistReq) (*pb.UserOperationResp, error) {
	if len(req.IpList) > maxIPListLen {
		return &pb.UserOperationResp{ErrMsg: "too many ips"}, nil
	}
	if err := l.svcCtx.AddBlacklist(req.IpList); err != nil {
		return &pb.UserOperationResp{ErrMsg: err.Error()}, nil
	}

	return &pb.UserOperationResp{Success: true}, nil
}
