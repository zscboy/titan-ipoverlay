package yuanren

import (
	"context"

	"titan-ipoverlay/manager/internal/svc"
	"titan-ipoverlay/manager/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type GetPopsLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewGetPopsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetPopsLogic {
	return &GetPopsLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetPopsLogic) GetPops() (resp *types.GetPopsResp, err error) {
	p := &types.Pop{ID: l.svcCtx.Config.Yuanren.Id,
		Name:        l.svcCtx.Config.Yuanren.Name,
		Area:        l.svcCtx.Config.Yuanren.Area,
		Socks5Addr:  l.svcCtx.Config.Yuanren.Socks5Addr,
		CountryCode: l.svcCtx.Config.Yuanren.CountryCode}
	return &types.GetPopsResp{Pops: []*types.Pop{p}}, nil

}
