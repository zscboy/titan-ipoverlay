package ws

import (
	"net/http"
	"titan-ipoverlay/ippop/model"

	"github.com/zeromicro/go-zero/rest/httpx"
)

type QoSHandler struct {
	tunMgr *TunnelManager
}

func NewQoSHandler(tunMgr *TunnelManager) *QoSHandler {
	return &QoSHandler{tunMgr: tunMgr}
}

type BlacklistAddReq struct {
	IPs    []string `json:"ips"`
	Reason string   `json:"reason"`
}

type BlacklistRemoveReq struct {
	IPs    []string `json:"ips"`
	Reason string   `json:"reason"`
}

type BlacklistAuditResp struct {
	Audits []string `json:"audits"`
}

type BlacklistListResp struct {
	IPs []string `json:"ips"`
}

// ServeBlacklistAdd 手动拉黑 IP
func (h *QoSHandler) ServeBlacklistAdd(w http.ResponseWriter, r *http.Request) {
	var req BlacklistAddReq
	if err := httpx.Parse(r, &req); err != nil {
		httpx.ErrorCtx(r.Context(), w, err)
		return
	}

	for _, ip := range req.IPs {
		// 手动拉黑，nodeID 标记为 manual
		h.tunMgr.BlacklistNode(ip, "manual", req.Reason)
	}

	httpx.Ok(w)
}

// ServeBlacklistRemove 手动移除黑名单
func (h *QoSHandler) ServeBlacklistRemove(w http.ResponseWriter, r *http.Request) {
	var req BlacklistRemoveReq
	if err := httpx.Parse(r, &req); err != nil {
		httpx.ErrorCtx(r.Context(), w, err)
		return
	}

	h.tunMgr.RemoveBlacklistNodes(req.IPs, req.Reason)
	httpx.Ok(w)
}

// ServeBlacklistClear 清空黑名单
func (h *QoSHandler) ServeBlacklistClear(w http.ResponseWriter, r *http.Request) {
	ips, err := model.GetBlacklist(h.tunMgr.redis)
	if err != nil {
		httpx.ErrorCtx(r.Context(), w, err)
		return
	}

	if len(ips) > 0 {
		h.tunMgr.RemoveBlacklistNodes(ips, "manual clear all")
	}
	httpx.Ok(w)
}

// ServeBlacklistAudit 获取黑名单审计日志
func (h *QoSHandler) ServeBlacklistAudit(w http.ResponseWriter, r *http.Request) {
	count := 100 // 默认返回最近 100 条
	audits, err := model.GetBlacklistAudits(h.tunMgr.redis, count)
	if err != nil {
		httpx.ErrorCtx(r.Context(), w, err)
		return
	}

	httpx.OkJsonCtx(r.Context(), w, &BlacklistAuditResp{Audits: audits})
}

// ServeBlacklistList 获取当前黑名单 IP 列表
func (h *QoSHandler) ServeBlacklistList(w http.ResponseWriter, r *http.Request) {
	ips, err := model.GetBlacklist(h.tunMgr.redis)
	if err != nil {
		httpx.ErrorCtx(r.Context(), w, err)
		return
	}

	httpx.OkJsonCtx(r.Context(), w, &BlacklistListResp{IPs: ips})
}
