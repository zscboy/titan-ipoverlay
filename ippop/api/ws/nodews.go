package ws

import (
	"net"
	"net/http"
	"strings"
	"titan-ipoverlay/ippop/api/internal/types"

	"github.com/gorilla/websocket"
	"github.com/zeromicro/go-zero/core/logx"
)

var (
	upgrader = websocket.Upgrader{} // use default options
)

type NodeWS struct {
	tunMgr *TunnelManager
}

func NewNodeWS(tunMgr *TunnelManager) *NodeWS {
	return &NodeWS{tunMgr: tunMgr}
}

func (ws *NodeWS) ServeWS(w http.ResponseWriter, r *http.Request, req *types.NodeWSReq) error {
	logx.Infof("NodeWS.ServeWS %s, %v", r.URL.Path, req)

	ip, err := ws.getRemoteIP(r)
	if err != nil {
		return err
	}

	c, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return err
	}
	defer c.Close()

	ws.tunMgr.acceptWebsocket(c, req, ip)

	return nil
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
