package ws

import (
	"context"
	"fmt"
	"time"
	"titan-ipoverlay/ippop/model"

	"github.com/zeromicro/go-zero/core/logx"
)

// AcquireExclusiveNode implements NodeSource interface
func (tm *TunnelManager) AcquireExclusiveNode(ctx context.Context) (string, *Tunnel, error) {
	ip, tun := tm.ipPool.AcquireIP()
	if tun == nil {
		return "", nil, fmt.Errorf("no free ip found in pool")
	}
	return ip, tun, nil
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
		// If the IP was marked for deactivation/blacklist while it was assigned,
		// now that it's released, we can safely kick its tunnels.
		if tm.ipPool.IsIPDeactivated(ip) {
			tunnels := tm.ipPool.GetTunnelsByIP(ip)
			for _, t := range tunnels {
				logx.Infof("IP %s was released and is deactivated, kicking tunnel %s", ip, t.opts.Id)
				t.waitClose()
			}
		}
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
