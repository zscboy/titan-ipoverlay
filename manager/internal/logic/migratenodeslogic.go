package logic

import (
	"context"

	"titan-ipoverlay/ippop/rpc/serverapi"
	"titan-ipoverlay/manager/internal/svc"
	"titan-ipoverlay/manager/internal/types"
	"titan-ipoverlay/manager/model"

	"github.com/zeromicro/go-zero/core/logx"
)

type MigrateNodesLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewMigrateNodesLogic(ctx context.Context, svcCtx *svc.ServiceContext) *MigrateNodesLogic {
	return &MigrateNodesLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *MigrateNodesLogic) MigrateNodes(req *types.MigrateNodesReq) (resp *types.UserOperationResp, err error) {
	sourcePop, ok := l.svcCtx.Pops[req.SourcePop]
	if !ok {
		return &types.UserOperationResp{Success: false, ErrMsg: "source pop not found"}, nil
	}
	_, ok = l.svcCtx.Pops[req.TargetPop]
	if !ok {
		return &types.UserOperationResp{Success: false, ErrMsg: "target pop not found"}, nil
	}

	// 1. Get free IPs from source POP
	freeIPsResp, err := sourcePop.API.GetFreeIPs(l.ctx, &serverapi.GetFreeIPsReq{
		Count:    int32(req.Count),
		FromHead: req.FromHead,
	})
	if err != nil {
		return &types.UserOperationResp{Success: false, ErrMsg: "failed to get free IPs: " + err.Error()}, nil
	}

	if len(freeIPsResp.Ips) == 0 {
		return &types.UserOperationResp{Success: false, ErrMsg: "no free nodes to migrate"}, nil
	}

	nodeIDToIP := make(map[string]string)
	ips := make([]string, 0, len(freeIPsResp.Ips))
	for _, ipInfo := range freeIPsResp.Ips {
		for _, nodeID := range ipInfo.NodeIds {
			nodeIDToIP[nodeID] = ipInfo.Ip
		}
		ips = append(ips, ipInfo.Ip)
	}

	// 2. Migrate nodes to target POP in Redis
	if err := model.BatchMoveNodesToPop(l.svcCtx.Redis, nodeIDToIP, req.SourcePop, req.TargetPop); err != nil {
		logx.Errorf("failed to batch migrate nodes from %s to %s: %v", req.SourcePop, req.TargetPop, err)
		return &types.UserOperationResp{Success: false, ErrMsg: "failed to migrate nodes in redis: " + err.Error()}, nil
	}

	// 3. Kick nodes in source POP
	_, err = sourcePop.API.KickNodeByIP(l.ctx, &serverapi.KickNodeByIPReq{
		IpList: ips,
	})
	if err != nil {
		logx.Errorf("failed to kick nodes in source pop: %v", err)
	}

	return &types.UserOperationResp{Success: true}, nil
}
