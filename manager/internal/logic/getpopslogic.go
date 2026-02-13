package logic

import (
	"context"

	"titan-ipoverlay/manager/internal/svc"
	"titan-ipoverlay/manager/internal/types"
	"titan-ipoverlay/manager/model"

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
	popIDs := make([]string, 0, len(l.svcCtx.Config.Pops))
	pops := make([]*types.Pop, 0, len(l.svcCtx.Config.Pops))
	for id, pop := range l.svcCtx.Pops {
		p := &types.Pop{ID: id, Name: pop.Config.Name, Area: pop.Config.Area, Socks5Addr: pop.Socks5Addr, CountryCode: pop.Config.CountryCode}
		pops = append(pops, p)

		popIDs = append(popIDs, id)
	}

	nodeCountMap, err := model.NodeCountOfPops(l.ctx, l.svcCtx.Redis, popIDs)
	if err != nil {
		return nil, err
	}

	for _, pop := range pops {
		cnt, ok := nodeCountMap[pop.ID]
		if ok {
			pop.TotalNode = int(cnt)
		}
	}

	return &types.GetPopsResp{Pops: pops}, nil
}
