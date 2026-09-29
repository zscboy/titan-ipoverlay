package ws

import (
	"fmt"
	"net"
	"net/http"
	"titan-ipoverlay/ippop/socks5"

	"github.com/zeromicro/go-zero/rest/httpx"
)

type Socks5ClientBlacklistHandler struct{ server *socks5.Socks5Server }

func NewSocks5ClientBlacklistHandler(server *socks5.Socks5Server) *Socks5ClientBlacklistHandler {
	return &Socks5ClientBlacklistHandler{server: server}
}

type Socks5BlacklistIPsReq struct {
	IPs []string `json:"ips"`
}
type Socks5BlacklistListResp struct {
	IPs []string `json:"ips"`
}

func parseBlacklistIPs(values []string) ([]net.IP, error) {
	ips := make([]net.IP, 0, len(values))
	for _, value := range values {
		ip := net.ParseIP(value)
		if ip == nil {
			return nil, fmt.Errorf("invalid IP address %q", value)
		}
		ips = append(ips, ip)
	}
	return ips, nil
}

func isLocalhostRequest(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	return err == nil && host == "127.0.0.1"
}

func (h *Socks5ClientBlacklistHandler) requireLocalhost(w http.ResponseWriter, r *http.Request) bool {
	if isLocalhostRequest(r) {
		return true
	}
	http.Error(w, "forbidden: only 127.0.0.1 is allowed", http.StatusForbidden)
	return false
}

func (h *Socks5ClientBlacklistHandler) ServeAdd(w http.ResponseWriter, r *http.Request) {
	if !h.requireLocalhost(w, r) {
		return
	}
	var req Socks5BlacklistIPsReq
	if err := httpx.Parse(r, &req); err != nil {
		httpx.ErrorCtx(r.Context(), w, err)
		return
	}
	ips, err := parseBlacklistIPs(req.IPs)
	if err != nil {
		httpx.ErrorCtx(r.Context(), w, err)
		return
	}
	h.server.AddIPBlacklist(ips)
	httpx.Ok(w)
}

func (h *Socks5ClientBlacklistHandler) ServeRemove(w http.ResponseWriter, r *http.Request) {
	if !h.requireLocalhost(w, r) {
		return
	}
	var req Socks5BlacklistIPsReq
	if err := httpx.Parse(r, &req); err != nil {
		httpx.ErrorCtx(r.Context(), w, err)
		return
	}
	ips, err := parseBlacklistIPs(req.IPs)
	if err != nil {
		httpx.ErrorCtx(r.Context(), w, err)
		return
	}
	h.server.RemoveIPBlacklist(ips)
	httpx.Ok(w)
}

func (h *Socks5ClientBlacklistHandler) ServeList(w http.ResponseWriter, r *http.Request) {
	if !h.requireLocalhost(w, r) {
		return
	}
	httpx.OkJsonCtx(r.Context(), w, &Socks5BlacklistListResp{IPs: h.server.IPBlacklist()})
}
