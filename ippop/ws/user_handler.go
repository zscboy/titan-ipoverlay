package ws

import (
	"net"
	"net/http"

	"github.com/zeromicro/go-zero/rest/httpx"
)

type UserHandler struct {
	tunMgr *TunnelManager
}

func NewUserHandler(tunMgr *TunnelManager) *UserHandler {
	return &UserHandler{tunMgr: tunMgr}
}

type DeleteUserCacheReq struct {
	Username string `json:"username" form:"username"`
}

func (h *UserHandler) isLocalRequest(r *http.Request) bool {
	host, _, _ := net.SplitHostPort(r.RemoteAddr)
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	return ip.IsLoopback() || ip.IsPrivate()
}

func (h *UserHandler) ServeDeleteUserCache(w http.ResponseWriter, r *http.Request) {
	if !h.isLocalRequest(r) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	var req DeleteUserCacheReq
	if err := httpx.Parse(r, &req); err != nil {
		httpx.ErrorCtx(r.Context(), w, err)
		return
	}

	if req.Username == "" {
		req.Username = r.URL.Query().Get("username")
	}

	if req.Username == "" {
		http.Error(w, "username is required", http.StatusBadRequest)
		return
	}

	h.tunMgr.DeleteUserFromCache(req.Username)

	httpx.OkJsonCtx(r.Context(), w, map[string]interface{}{
		"code": 0,
		"msg":  "ok",
	})
}
