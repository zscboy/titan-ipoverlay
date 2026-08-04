package logic

import (
	"context"
	"fmt"

	"titan-ipoverlay/ippop/rpc/serverapi"
	"titan-ipoverlay/manager/internal/svc"
	"titan-ipoverlay/manager/internal/types"
	"titan-ipoverlay/manager/model"

	"github.com/zeromicro/go-zero/core/logx"
)

const maxMigrateNodeListSize = 100

type MigrateNodeListLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewMigrateNodeListLogic(ctx context.Context, svcCtx *svc.ServiceContext) *MigrateNodeListLogic {
	return &MigrateNodeListLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *MigrateNodeListLogic) MigrateNodeList(req *types.MigrateNodeListReq) (resp *types.UserOperationResp, err error) {
	_, ok := l.svcCtx.Pops[req.TargetPop]
	if !ok {
		return &types.UserOperationResp{Success: false, ErrMsg: "target pop not found"}, nil
	}

	if len(req.Nodes) == 0 {
		return &types.UserOperationResp{Success: true}, nil
	}

	if len(req.Nodes) > maxMigrateNodeListSize {
		return &types.UserOperationResp{Success: false, ErrMsg: fmt.Sprintf("nodes list exceeds maximum limit of %d", maxMigrateNodeListSize)}, nil
	}

	nodeIDs := make([]string, 0, len(req.Nodes))
	for _, nodeItem := range req.Nodes {
		nodeIDs = append(nodeIDs, nodeItem.ID)
	}

	nodePopMap, err := model.GetNodePopIPs(l.svcCtx.Redis, nodeIDs)
	if err != nil {
		logx.Errorf("failed to batch get pop for nodes: %v", err)
		return &types.UserOperationResp{Success: false, ErrMsg: "failed to retrieve source pop info: " + err.Error()}, nil
	}

	// 1. Group nodes by their current Pop ID
	// Group format: sourcePopID -> map[nodeID]ip
	groups := make(map[string]map[string]string)

	for _, nodeItem := range req.Nodes {
		nodeID := nodeItem.ID
		ip := nodeItem.IP
		popID := nodePopMap[nodeID]

		if len(popID) == 0 || len(ip) == 0 {
			logx.Errorf("node %s has empty pop (%s) or ip (%s)", nodeID, popID, ip)
			continue
		}

		if _, exists := groups[popID]; !exists {
			groups[popID] = make(map[string]string)
		}
		groups[popID][nodeID] = ip
	}

	// 2. Process each group
	for sourcePopID, nodeIDToIP := range groups {
		if len(nodeIDToIP) == 0 {
			continue
		}

		// Migrate nodes to target POP in Redis
		if err := model.BatchMoveNodesToPop(l.svcCtx.Redis, nodeIDToIP, sourcePopID, req.TargetPop); err != nil {
			logx.Errorf("failed to batch migrate nodes from %s to %s: %v", sourcePopID, req.TargetPop, err)
			return &types.UserOperationResp{Success: false, ErrMsg: "failed to migrate nodes in redis: " + err.Error()}, nil
		}

		// Clear local memory cache in manager for these nodes
		for nodeID := range nodeIDToIP {
			l.svcCtx.NodePopCache.Delete(nodeID)
		}

		// Request the source POP to kick nodes offline
		sourcePop, ok := l.svcCtx.Pops[sourcePopID]
		if !ok {
			logx.Errorf("source pop %s not found in manager config, skip kicking", sourcePopID)
			continue
		}

		// Collect IPs
		ips := make([]string, 0, len(nodeIDToIP))
		for _, ip := range nodeIDToIP {
			ips = append(ips, ip)
		}

		_, err = sourcePop.API.KickNodeByIP(l.ctx, &serverapi.KickNodeByIPReq{
			IpList: ips,
		})
		if err != nil {
			logx.Errorf("failed to kick nodes in source pop %s: %v", sourcePopID, err)
		}
	}


	return &types.UserOperationResp{Success: true}, nil
}

