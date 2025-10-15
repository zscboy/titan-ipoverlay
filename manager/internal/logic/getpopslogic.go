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
	pops := make([]*types.Pop, 0, len(l.svcCtx.Config.Pops))
	for id, server := range l.svcCtx.Servers {
		count, err := model.NodeCountOfPop(l.svcCtx.Redis, id)
		if err != nil {
			logx.Errorf("count pop %s node failed:%v", id, err.Error())
		}

		p := &types.Pop{ID: id, Area: server.Area, TotalNode: int(count), Socks5Addr: server.Socks5Addr}
		pops = append(pops, p)
	}
	return &types.GetPopsResp{Pops: pops}, nil
}
