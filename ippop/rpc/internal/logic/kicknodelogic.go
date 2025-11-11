package logic

import (
	"context"

	"titan-ipoverlay/ippop/rpc/internal/svc"
	"titan-ipoverlay/ippop/rpc/pb"

	"github.com/zeromicro/go-zero/core/logx"
)

type KickNodeLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewKickNodeLogic(ctx context.Context, svcCtx *svc.ServiceContext) *KickNodeLogic {
	return &KickNodeLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *KickNodeLogic) KickNode(in *pb.KickNodeReq) (*pb.UserOperationResp, error) {
	return l.kickNode(in.NodeId)
}

func (l *KickNodeLogic) kickNode(nodeId string) (*pb.UserOperationResp, error) {
	if err := l.svcCtx.Kick(nodeId); err != nil {
		return &pb.UserOperationResp{ErrMsg: err.Error()}, nil
	}
	return &pb.UserOperationResp{Success: true}, nil
}
