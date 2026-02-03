package logic

import (
	"context"

	"titan-ipoverlay/ippop/rpc/internal/svc"
	"titan-ipoverlay/ippop/rpc/pb"

	"github.com/zeromicro/go-zero/core/logx"
)

type KickNodeByIPLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewKickNodeByIPLogic(ctx context.Context, svcCtx *svc.ServiceContext) *KickNodeByIPLogic {
	return &KickNodeByIPLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *KickNodeByIPLogic) KickNodeByIP(in *pb.KickNodeByIPReq) (*pb.UserOperationResp, error) {
	if len(in.IpList) > maxIPListLen {
		return &pb.UserOperationResp{Success: false, ErrMsg: "too many ips"}, nil
	}

	if err := l.svcCtx.KickByIPs(in.IpList); err != nil {
		return &pb.UserOperationResp{Success: false, ErrMsg: err.Error()}, nil
	}

	return &pb.UserOperationResp{Success: true}, nil
}
