package ws

import (
	"net/http"

	"github.com/zeromicro/go-zero/rest/httpx"
)

type IPPoolQuery struct {
	tunMgr *TunnelManager
}

func NewIPPoolQuery(tunMgr *TunnelManager) *IPPoolQuery {
	return &IPPoolQuery{tunMgr: tunMgr}
}

type GetFreeIPsResp struct {
	IPs []FreeIPInfo `json:"ips"`
}

func (q *IPPoolQuery) ServeFreeIPs(w http.ResponseWriter, r *http.Request) {
	ips := q.tunMgr.ipPool.GetFreeIPsFromTail(100)
	httpx.OkJsonCtx(r.Context(), w, &GetFreeIPsResp{IPs: ips})
}
