package ws

import (
	"encoding/csv"
	"net/http"
	"time"

	"github.com/zeromicro/go-zero/core/logx"
)

const interval = 5

type NodeListQuery struct {
	tunMgr   *TunnelManager
	lastTime time.Time
}

func NewNodeListQuery(tunMgr *TunnelManager) *NodeListQuery {
	return &NodeListQuery{
		tunMgr:   tunMgr,
		lastTime: time.Now(),
	}
}

func (q *NodeListQuery) ServeNodeListCSV(w http.ResponseWriter, r *http.Request) {
	logx.Infof("ServeNodeListCSV, remote:%s, since:%f", r.RemoteAddr, time.Since(q.lastTime).Seconds())

	if time.Duration(time.Since(q.lastTime).Seconds()) < interval {
		http.Error(w, "Too Many Requests (Limit: 1 per 5s)", http.StatusTooManyRequests)
		return
	}
	q.lastTime = time.Now()

	w.Header().Set("Content-Type", "text/csv")
	w.Header().Set("Content-Disposition", "attachment;filename=online_nodes.csv")

	type NodeInfo struct {
		ID string
		IP string
	}

	nodes := make([]NodeInfo, 0)
	q.tunMgr.tunnels.Range(func(key, value interface{}) bool {
		tun := value.(*Tunnel)
		nodes = append(nodes, NodeInfo{
			ID: tun.opts.Id,
			IP: tun.opts.IP,
		})
		return true
	})

	writer := csv.NewWriter(w)
	defer writer.Flush()

	// Write CSV Header
	writer.Write([]string{"ID", "IP"})

	for _, tun := range nodes {
		row := []string{
			tun.ID,
			tun.IP,
		}
		if err := writer.Write(row); err != nil {
			logx.Errorf("NodeListQuery CSV write row error: %v", err)
			return
		}
	}
}
