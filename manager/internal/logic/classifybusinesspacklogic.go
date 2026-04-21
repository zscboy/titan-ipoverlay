package logic

import (
	"context"

	"titan-ipoverlay/manager/internal/svc"
	"titan-ipoverlay/manager/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type ClassifyBusinessPackLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewClassifyBusinessPackLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ClassifyBusinessPackLogic {
	return &ClassifyBusinessPackLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *ClassifyBusinessPackLogic) ClassifyBusinessPack(req *types.ClassifyBusinessPackReq) (*types.ClassifyBusinessPackResp, error) {
	pack := l.svcCtx.PackClassifier.Classify(req.Host, req.RequestedPack)
	return &types.ClassifyBusinessPackResp{
		Host: req.Host,
		Pack: pack,
	}, nil
}
