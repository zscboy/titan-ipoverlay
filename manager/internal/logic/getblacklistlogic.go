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

func (l *GetBlackListLogic) GetBlackList(req *types.GetBlacklistReq) (resp *types.GetBlacklistResp, err error) {
	server := l.svcCtx.Pops[req.PopID]
	if server == nil {
		return nil, fmt.Errorf("popid %s not exist", req.PopID)
	}

	getBlacklistResp, err := server.API.GetBlacklist(l.ctx, &serverapi.GetBlacklistReq{
		Cursor: req.Cursor,
		Count:  uint32(req.Count),
	})
	if err != nil {
		return nil, err
	}

	return &types.GetBlacklistResp{
		IPList:     getBlacklistResp.IpList,
		NextCursor: getBlacklistResp.NextCursor,
	}, nil
}
