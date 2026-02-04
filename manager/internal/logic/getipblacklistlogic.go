package logic

import (
	"context"

	"titan-ipoverlay/manager/internal/svc"
	"titan-ipoverlay/manager/internal/types"
	"titan-ipoverlay/manager/model"

	"github.com/zeromicro/go-zero/core/logx"
)

type GetIPBlacklistLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewGetIPBlacklistLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetIPBlacklistLogic {
	return &GetIPBlacklistLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetIPBlacklistLogic) GetIPBlacklist(req *types.GetIPBlacklistReq) (resp *types.GetIPBlacklistResp, err error) {
	ips, nextCursor, total, err := model.GetIPBlacklist(l.svcCtx.Redis, req.Cursor, req.Count)
	if err != nil {
		return nil, err
	}

	return &types.GetIPBlacklistResp{
		IPList:     ips,
		NextCursor: nextCursor,
		Total:      total,
	}, nil
}
