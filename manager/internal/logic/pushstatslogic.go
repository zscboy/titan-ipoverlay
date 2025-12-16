package logic

import (
	"context"

	"titan-ipoverlay/manager/internal/push"
	"titan-ipoverlay/manager/internal/svc"
	"titan-ipoverlay/manager/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type PushStatsLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewPushStatsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *PushStatsLogic {
	return &PushStatsLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *PushStatsLogic) PushStats(req *types.PusStatsReq) error {
	// todo: add your logic here and delete this line
	l.svcCtx.PushManager.Push(push.Stats{Type: req.Type, Data: req.Payload})
	return nil
}
