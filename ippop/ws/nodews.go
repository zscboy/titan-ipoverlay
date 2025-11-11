package ws

import (
	"net"
	"net/http"
	"strings"

	"github.com/gorilla/websocket"
	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/rest/httpx"
)

var (
	upgrader = websocket.Upgrader{} // use default options
)

type NodeWSReq struct {
	NodeId  string `form:"id"`
	OS      string `form:"os"`
	Version string `form:"version,optional"`
}

type NodeWS struct {
	tunMgr *TunnelManager
}

func NewNodeWS(tunMgr *TunnelManager) *NodeWS {
	return &NodeWS{tunMgr: tunMgr}
}

func (ws *NodeWS) ServeWS(w http.ResponseWriter, r *http.Request) {
	logx.Infof("NodeWS.ServeWS %s", r.URL.Path)

	var req NodeWSReq
	if err := httpx.Parse(r, &req); err != nil {
		httpx.ErrorCtx(r.Context(), w, err)
		return
	}

	ip, err := ws.getRemoteIP(r)
	if err != nil {
		httpx.ErrorCtx(r.Context(), w, err)
		return
	}

	c, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		httpx.ErrorCtx(r.Context(), w, err)
		return
	}
	defer c.Close()

	ws.tunMgr.acceptWebsocket(c, &req, ip)

}

func (ws *NodeWS) getRemoteIP(r *http.Request) (string, error) {
	ip := r.Header.Get("X-Real-IP")
	if len(ip) != 0 {
		return ip, nil
	}

	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		ips := strings.Split(xff, ",")
		for _, ip := range ips {
			ip = strings.TrimSpace(ip)
			return ip, nil
		}
	}

	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return "", err
	}
	return ip, nil
}
