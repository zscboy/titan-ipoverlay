package logic

import (
	"context"
	"fmt"

	"titan-ipoverlay/ippop/rpc/serverapi"
	"titan-ipoverlay/manager/internal/svc"
	"titan-ipoverlay/manager/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type ListNodeLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewListNodeLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ListNodeLogic {
	return &ListNodeLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *ListNodeLogic) ListNode(req *types.ListNodeReq) (resp *types.ListNodeResp, err error) {
	server := l.svcCtx.Pops[req.PopID]
	if server == nil {
		return nil, fmt.Errorf("pop %s not found", req.PopID)
	}

	in := &serverapi.ListNodeReq{
		Type:  int32(req.Type),
		Start: int32(req.Start),
		End:   int32(req.End),
	}

	listNodeResp, err := server.API.ListNode(l.ctx, in)
	if err != nil {
		return nil, err
	}

	nodes := make([]*types.Node, 0, len(listNodeResp.Nodes))
	for _, node := range listNodeResp.Nodes {
		nodes = append(nodes, toNodeResp(node))
	}

	return &types.ListNodeResp{Nodes: nodes, Total: int(listNodeResp.Total)}, nil
}
