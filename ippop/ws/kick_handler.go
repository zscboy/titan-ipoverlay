package ws

import (
	"net/http"

	"github.com/zeromicro/go-zero/rest/httpx"
)

type KickHandler struct {
	tunMgr *TunnelManager
}

func NewKickHandler(tunMgr *TunnelManager) *KickHandler {
	return &KickHandler{tunMgr: tunMgr}
}

type KickIPsReq struct {
	IPs []string `json:"ips"`
}

// ServeKickIPs 剔除指定 IP 列表对应的连接
func (h *KickHandler) ServeKickIPs(w http.ResponseWriter, r *http.Request) {
	var req KickIPsReq
	if err := httpx.Parse(r, &req); err != nil {
		httpx.ErrorCtx(r.Context(), w, err)
		return
	}

	if err := h.tunMgr.KickByIPs(req.IPs); err != nil {
		httpx.ErrorCtx(r.Context(), w, err)
		return
	}

	httpx.Ok(w)
}

type KickQueueLengthResp struct {
	Length int `json:"length"`
}

// ServeGetKickQueueLength 查询等待剔除的 IP 队列长度
func (h *KickHandler) ServeGetKickQueueLength(w http.ResponseWriter, r *http.Request) {
	length := h.tunMgr.GetKickQueueLength()
	httpx.OkJsonCtx(r.Context(), w, &KickQueueLengthResp{Length: length})
}
