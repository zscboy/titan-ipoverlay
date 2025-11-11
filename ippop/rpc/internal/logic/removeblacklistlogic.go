package logic

import (
	"context"

	"titan-ipoverlay/ippop/model"
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
	return l.removeBlacklist(in.NodeId)
}

func (l *RemoveBlacklistLogic) removeBlacklist(nodeId string) (*pb.UserOperationResp, error) {
	if err := model.RemoveBlacklist(l.svcCtx.Redis, nodeId); err != nil {
		return &pb.UserOperationResp{ErrMsg: err.Error()}, nil
	}

	err := l.svcCtx.Kick(nodeId)
	if err != nil {
		logx.Alert("RemoveBlacklistLogic.removeBlacklist: " + err.Error())
	}

	return &pb.UserOperationResp{Success: true}, nil
}
