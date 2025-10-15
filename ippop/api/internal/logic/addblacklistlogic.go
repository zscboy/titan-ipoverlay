package logic

import (
	"context"

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
	if err := model.AddBlacklist(l.svcCtx.Redis, req.NodeID); err != nil {
		return err
	}

	err := l.svcCtx.TunMgr.KickNode(req.NodeID)
	if err != nil {
		logx.Infof("AddBlackListLogic.AddBlackList: %s", err.Error())
	}

	return nil
}
