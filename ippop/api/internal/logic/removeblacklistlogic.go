package logic

import (
	"context"

	"titan-ipoverlay/ippop/api/internal/svc"
	"titan-ipoverlay/ippop/api/internal/types"
	"titan-ipoverlay/ippop/api/model"

	"github.com/zeromicro/go-zero/core/logx"
)

type RemoveBlackListLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewRemoveBlackListLogic(ctx context.Context, svcCtx *svc.ServiceContext) *RemoveBlackListLogic {
	return &RemoveBlackListLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *RemoveBlackListLogic) RemoveBlackList(req *types.RemoveBlackListReq) error {
	if err := model.RemoveBlacklist(l.svcCtx.Redis, req.NodeID); err != nil {
		return err
	}

	err := l.svcCtx.TunMgr.KickNode(req.NodeID)
	if err != nil {
		logx.Infof("AddBlackListLogic.AddBlackList: %s", err.Error())
	}

	return nil
}
