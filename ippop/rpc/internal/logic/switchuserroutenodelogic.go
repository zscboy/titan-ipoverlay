package logic

import (
	"context"
	"fmt"

	"titan-ipoverlay/ippop/model"
	"titan-ipoverlay/ippop/rpc/internal/svc"
	"titan-ipoverlay/ippop/rpc/pb"

	"github.com/zeromicro/go-zero/core/logx"
)

type SwitchUserRouteNodeLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewSwitchUserRouteNodeLogic(ctx context.Context, svcCtx *svc.ServiceContext) *SwitchUserRouteNodeLogic {
	return &SwitchUserRouteNodeLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

func (l *SwitchUserRouteNodeLogic) SwitchUserRouteNode(in *pb.SwitchUserRouteNodeReq) (*pb.UserOperationResp, error) {
	user, err := model.GetUser(l.svcCtx.Redis, in.UserName)
	if err != nil {
		return &pb.UserOperationResp{ErrMsg: err.Error()}, nil
	}

	if user == nil {
		return &pb.UserOperationResp{ErrMsg: fmt.Sprintf("user %s not exist", in.UserName)}, nil
	}

	if len(in.NodeId) != 0 {
		if err := l.checkNode(in.NodeId); err != nil {
			return &pb.UserOperationResp{ErrMsg: err.Error()}, nil
		}
	} else {
		nodeID, err := model.AllocateFreeNode(l.ctx, l.svcCtx.Redis)
		if err != nil {
			return &pb.UserOperationResp{ErrMsg: err.Error()}, nil
		}
		in.NodeId = string(nodeID)
	}

	if err := model.SwitchNodeByUser(l.ctx, l.svcCtx.Redis, user, in.NodeId); err != nil {
		if err2 := model.AddFreeNode(l.svcCtx.Redis, in.NodeId); err2 != nil {
			logx.Errorf("SwitchUserRouteNode AddFreeNode %v", err.Error())
		}
		return &pb.UserOperationResp{ErrMsg: err.Error()}, nil
	}

	if err := l.svcCtx.DeleteCache(in.UserName); err != nil {
		return &pb.UserOperationResp{ErrMsg: err.Error()}, nil
	}

	return &pb.UserOperationResp{Success: true}, nil
}

func (l *SwitchUserRouteNodeLogic) checkNode(nodeID string) error {
	node, err := model.GetNode(l.ctx, l.svcCtx.Redis, nodeID)
	if err != nil {
		return err
	}

	if node == nil {
		return fmt.Errorf("node %s not exist", nodeID)
	}

	if len(node.BindUser) != 0 {
		return fmt.Errorf("node %s alreay used by user %s", nodeID, node.BindUser)
	}

	if !node.Online {
		return fmt.Errorf("node %s offline", nodeID)
	}

	return nil
}
