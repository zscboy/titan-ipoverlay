package ws

import (
	"context"
	"fmt"
	"time"
	"titan-ipoverlay/ippop/config"
	"titan-ipoverlay/ippop/model"

	"github.com/zeromicro/go-zero/core/logx"
)

// AcquireExclusiveNode implements NodeSource interface
func (tm *TunnelManager) AcquireExclusiveNode(ctx context.Context) (string, *Tunnel, error) {
	if tm.config.WS.NodeAllocateStrategy == config.NodeAllocateIPPool {
		ip, tun := tm.ipPool.AcquireIP()
		if tun == nil {
			return "", nil, fmt.Errorf("no free ip found in pool")
		}
		return ip, tun, nil
	}

	nodeIDBytes, err := model.AllocateFreeNode(ctx, tm.redis)
	if err != nil {
		return "", nil, err
	}
	nodeID := string(nodeIDBytes)
	v, ok := tm.tunnels.Load(nodeID)
	if !ok {
		// If tunnel is not online locally, we don't put it back to free pool
		// because the free pool should only contain online/available nodes.
		logx.Errorf("AcquireExclusiveNode error: node %s allocated but not found in local tunnels", nodeID)
		return "", nil, fmt.Errorf("node %s allocated but not found in local tunnels", nodeID)
	}
	return "", v.(*Tunnel), nil
}

// ReleaseExclusiveNodes implements NodeSource interface
func (tm *TunnelManager) ReleaseExclusiveNodes(nodeIDs []string, ips []string) {
	now := time.Now()
	// Only release nodes that are still online locally
	if len(nodeIDs) > 0 {
		onlineNodes := make([]string, 0, len(nodeIDs))
		for _, id := range nodeIDs {
			if _, ok := tm.tunnels.Load(id); ok {
				onlineNodes = append(onlineNodes, id)
			}
		}

		if len(onlineNodes) > 0 {
			err := model.AddFreeNodes(context.Background(), tm.redis, onlineNodes)
			if err != nil {
				logx.Errorf("ReleaseExclusiveNodes failed: %v", err)
			}
		}
	}

	for _, ip := range ips {
		tm.ipPool.ReleaseIP(ip)
	}

	logx.Infof("TunnelManager.ReleaseExclusiveNodes nodeIDs len:%d, ips len:%d cost %v", len(nodeIDs), len(ips), time.Since(now))
}

// GetLocalTunnel implements NodeSource interface
func (tm *TunnelManager) GetLocalTunnel(nodeID string) *Tunnel {
	v, ok := tm.tunnels.Load(nodeID)
	if !ok {
		return nil
	}
	return v.(*Tunnel)
}
