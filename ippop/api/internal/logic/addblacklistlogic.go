package logic

import (
	"context"
	"fmt"

	"titan-ipoverlay/ippop/api/internal/svc"
	"titan-ipoverlay/ippop/api/internal/types"
	"titan-ipoverlay/ippop/api/model"

	"github.com/zeromicro/go-zero/core/logx"
)

type AddBlackListLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewAddBlackListLogic(ctx context.Context, svcCtx *svc.ServiceContext) *AddBlackListLogic {
	return &AddBlackListLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *AddBlackListLogic) AddBlackList(req *types.AddBlackListReq) error {
	node, err := model.GetNode(l.svcCtx.Redis, req.NodeID)
	if err != nil {
		return err
	}

	if node == nil {
		return fmt.Errorf("node %s not exist", req.NodeID)
	}

	if len(node.BindUser) != 0 {
		if err := l.switchNodeForUser(node.BindUser); err != nil {
			return fmt.Errorf("node %s already bind by user %s, switch new node for user failed:%v", req.NodeID, node.BindUser, err)
		}
	}

	if err := model.AddBlacklist(l.svcCtx.Redis, req.NodeID); err != nil {
		return err
	}

	err = l.svcCtx.TunMgr.KickNode(req.NodeID)
	if err != nil {
		logx.Infof("AddBlackListLogic.AddBlackList: %s", err.Error())
	}

	return nil
}

func (l *AddBlackListLogic) switchNodeForUser(userName string) error {
	user, err := model.GetUser(l.svcCtx.Redis, userName)
	if err != nil {
		return err
	}

	if user == nil {
		return fmt.Errorf("user %s not exist", userName)
	}

	nodeIDBytes, err := model.AllocateFreeNode(l.ctx, l.svcCtx.Redis)
	if err != nil {
		return err
	}

	nodeID := string(nodeIDBytes)
	if err := model.SwitchNodeByUser(l.ctx, l.svcCtx.Redis, user, nodeID); err != nil {
		if err2 := model.AddFreeNode(l.svcCtx.Redis, nodeID); err2 != nil {
			logx.Errorf("SwitchUserRouteNode AddFreeNode %v", err.Error())
		}
		return err
	}

	l.svcCtx.TunMgr.DeleteUserFromCache(userName)
	return nil
}
