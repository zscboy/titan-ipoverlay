package logic

import (
	"context"

	"titan-ipoverlay/ippop/api/internal/svc"
	"titan-ipoverlay/ippop/api/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type KickNodeLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewKickNodeLogic(ctx context.Context, svcCtx *svc.ServiceContext) *KickNodeLogic {
	return &KickNodeLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *KickNodeLogic) KickNode(req *types.KickNodeReq) error {
	return l.svcCtx.TunMgr.KickNode(req.NodeID)
}
