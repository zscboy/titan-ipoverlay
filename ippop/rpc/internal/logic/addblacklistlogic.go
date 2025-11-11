package logic

import (
	"context"
	"fmt"

	"titan-ipoverlay/ippop/model"
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

func (l *AddBlacklistLogic) AddBlacklist(req *pb.AddBlacklistReq) (*pb.UserOperationResp, error) {
	node, err := model.GetNode(l.ctx, l.svcCtx.Redis, req.NodeId)
	if err != nil {
		return &pb.UserOperationResp{ErrMsg: err.Error()}, nil
	}

	if node == nil {
		return &pb.UserOperationResp{ErrMsg: fmt.Sprintf("node %s not exist", req.NodeId)}, nil
	}

	if len(node.BindUser) != 0 {
		if err := l.svcCtx.SwitchNode(node.BindUser); err != nil {
			return &pb.UserOperationResp{ErrMsg: fmt.Sprintf("node %s already bind by user %s, switch new node for user failed:%v", req.NodeId, node.BindUser, err)}, nil
		}
	}

	if err := model.AddBlacklist(l.svcCtx.Redis, req.NodeId); err != nil {
		return &pb.UserOperationResp{ErrMsg: err.Error()}, nil
	}

	err = l.svcCtx.Kick(req.NodeId)
	if err != nil {
		logx.Infof("AddBlackListLogic.AddBlackList: %s", err.Error())
	}

	return &pb.UserOperationResp{Success: true}, nil
}
