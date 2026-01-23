package ws

import (
	"fmt"
	"net/http"

	"github.com/zeromicro/go-zero/rest/httpx"
)

type SessionQueryReq struct {
	Username  string `form:"username"`
	SessionID string `form:"session"`
}

type SessionQueryResp struct {
	NodeID string `json:"node_id"`
	NodeIP string `json:"node_ip"`
}

type SessionQuery struct {
	tunMgr *TunnelManager
}

func NewSessionQuery(tunMgr *TunnelManager) *SessionQuery {
	return &SessionQuery{tunMgr: tunMgr}
}

func (sq *SessionQuery) ServeSessionQuery(w http.ResponseWriter, r *http.Request) {
	var req SessionQueryReq
	if err := httpx.Parse(r, &req); err != nil {
		httpx.ErrorCtx(r.Context(), w, err)
		return
	}

	if req.Username == "" {
		httpx.ErrorCtx(r.Context(), w, fmt.Errorf("username is required"))
		return
	}

	if req.SessionID == "" {
		httpx.ErrorCtx(r.Context(), w, fmt.Errorf("session is required"))
		return
	}

	sess := sq.tunMgr.sessionManager.GetSession(req.Username, req.SessionID)

	if sess == nil {
		httpx.ErrorCtx(r.Context(), w, fmt.Errorf("session %s not found", req.SessionID))
		return
	}

	tun := sq.tunMgr.GetLocalTunnel(sess.deviceID)
	if tun == nil {
		httpx.ErrorCtx(r.Context(), w, fmt.Errorf("node %s for session %s not online locally", sess.deviceID, req.SessionID))
		return
	}

	resp := &SessionQueryResp{
		NodeID: tun.opts.Id,
		NodeIP: tun.opts.IP,
	}
	httpx.OkJsonCtx(r.Context(), w, resp)
}
