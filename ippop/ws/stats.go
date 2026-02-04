package ws

import (
	"net/http"

	"github.com/zeromicro/go-zero/rest/httpx"
)

type PopStats struct {
	NodeCount        int `json:"node_count"`
	TotalIPCount     int `json:"total_ip_count"`
	FreeIPCount      int `json:"free_ip_count"`
	BlacklistIPCount int `json:"blacklist_ip_count"`
	AssignedIPCount  int `json:"assigned_ip_count"`
	UserSessionCount int `json:"user_session_count"`
	UserConnCount    int `json:"user_conn_count"`
}

type StatsQuery struct {
	tm *TunnelManager
}

func NewStatsQuery(tm *TunnelManager) *StatsQuery {
	return &StatsQuery{tm: tm}
}

func (s *StatsQuery) ServeStats(w http.ResponseWriter, r *http.Request) {
	poolStats := s.tm.ipPool.GetPoolStats()
	sessionCount := s.tm.sessionManager.SessionLen()

	resp := &PopStats{
		NodeCount:        poolStats.TunnelCount,
		TotalIPCount:     poolStats.TotalIPCount,
		FreeIPCount:      poolStats.FreeIPCount,
		BlacklistIPCount: poolStats.BlacklistIPCount,
		AssignedIPCount:  poolStats.AssignedIPCount,
		UserSessionCount: sessionCount,
		UserConnCount:    int(s.tm.socks5ConnCount.Load()),
	}

	httpx.OkJsonCtx(r.Context(), w, resp)
}
