package logic

import (
	"context"
	"fmt"

	"titan-ipoverlay/ippop/rpc/serverapi"
	"titan-ipoverlay/manager/internal/svc"
	"titan-ipoverlay/manager/internal/types"

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

func (l *GetBlackListLogic) GetBlackList(req *types.GetBlackListReq) (resp *types.GetBlackListResp, err error) {
	server := l.svcCtx.Servers[string(req.PodID)]
	if server == nil {
		return nil, fmt.Errorf("")
	}

	getBlackListResp, err := server.API.GetBlacklist(l.ctx, &serverapi.Empty{})
	if err != nil {
		return nil, err
	}

	return &types.GetBlackListResp{Nodes: getBlackListResp.Nodes}, nil
}
