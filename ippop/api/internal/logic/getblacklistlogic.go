package logic

import (
	"context"

	"titan-ipoverlay/ippop/api/internal/svc"
	"titan-ipoverlay/ippop/api/internal/types"
	"titan-ipoverlay/ippop/api/model"

	"github.com/zeromicro/go-zero/core/logx"
)

type GetBlackListLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewGetBlackListLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetBlackListLogic {
	return &GetBlackListLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetBlackListLogic) GetBlackList() (resp *types.GetBlackListResp, err error) {
	nodes, err := model.GetBlacklist(l.svcCtx.Redis)
	if err != nil {
		return nil, err
	}

	return &types.GetBlackListResp{NodeIDs: nodes}, nil
}
