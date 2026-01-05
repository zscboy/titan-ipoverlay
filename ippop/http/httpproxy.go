package http

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
	"titan-ipoverlay/ippop/socks5"
	"titan-ipoverlay/ippop/ws"

	"github.com/zeromicro/go-zero/core/logx"
)

const (
	maxBodySize = 8 << 20 // 8MB
)

type HttpProxy struct {
	tunMgr *ws.TunnelManager
}

func NewHttProxy(tunMgr *ws.TunnelManager) *HttpProxy {
	return &HttpProxy{tunMgr: tunMgr}
}

func (p *HttpProxy) HandleProxy(w http.ResponseWriter, r *http.Request) {
	logx.Debug("HandleProxy")
	usernameBytes, passwdBytes, err := p.parseUserPassword(r.Header.Get("Proxy-Authorization"))
	if err == nil {
		err = p.tunMgr.HandleUserAuth(string(usernameBytes), string(passwdBytes))
	}

	if err != nil {
		logx.Errorf("user auth %v", err.Error())
		w.Header().Set("Proxy-Authenticate", `Basic realm="Restricted"`)
		w.WriteHeader(http.StatusProxyAuthRequired)
		_, _ = w.Write([]byte("407 Proxy Authentication Required\n"))
		return
	}

	username := string(usernameBytes)

	if r.Method == http.MethodConnect {
		p.handleHTTPS(w, r, username)
	} else {
		p.handleHTTP(w, r, username)
	}
}

func (p *HttpProxy) handleHTTPS(w http.ResponseWriter, r *http.Request, username string) {
	host, port, err := p.parseHostPort(r.Host, 443)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	hij, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "Hijacking not supported", http.StatusInternalServerError)
		return
	}

	conn, _, err := hij.Hijack()
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	defer conn.Close()

	_, _ = conn.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n"))

	tcpConn, ok := conn.(*net.TCPConn)
	if !ok {
		logx.Errorf("not a tcp connection")
		return
	}

	p.tunMgr.HandleSocks5TCP(tcpConn, &socks5.SocksTargetInfo{
		DomainName:     host,
		Port:           port,
		Username:       username,
		ConnCreateTime: time.Now(),
	})
}

func (p *HttpProxy) handleHTTP(w http.ResponseWriter, r *http.Request, username string) {
	host, port, err := p.parseHostPort(r.Host, 80)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	hij, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "Hijacking not supported", http.StatusInternalServerError)
		return
	}

	conn, _, err := hij.Hijack()
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	defer conn.Close()

	// Reconstruct the request
	r.Header.Del("Proxy-Authorization")
	r.RequestURI = "" // Let r.Write use r.URL

	var buf bytes.Buffer
	if err := r.Write(&buf); err != nil {
		logx.Errorf("r.Write failed: %v", err)
		return
	}

	tcpConn, ok := conn.(*net.TCPConn)
	if !ok {
		logx.Errorf("not a tcp connection")
		return
	}

	p.tunMgr.HandleSocks5TCP(tcpConn, &socks5.SocksTargetInfo{
		DomainName:     host,
		Port:           port,
		Username:       username,
		ExtraBytes:     buf.Bytes(),
		ConnCreateTime: time.Now(),
	})
}

func (p *HttpProxy) parseHostPort(hostPort string, defaultPort int) (string, int, error) {
	host, portStr, err := net.SplitHostPort(hostPort)
	if err != nil {
		if strings.Contains(err.Error(), "missing port") {
			return hostPort, defaultPort, nil
		}
		return hostPort, 0, err
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return host, 0, err
	}
	return host, port, nil
}

// return userName
func (p *HttpProxy) parseUserPassword(auth string) ([]byte, []byte, error) {
	if !strings.HasPrefix(auth, "Basic ") {
		return nil, nil, fmt.Errorf("client not include Proxy-Authorization Basic ")
	}
	payload, err := base64.StdEncoding.DecodeString(auth[len("Basic "):])
	if err != nil {
		return nil, nil, fmt.Errorf("decode Basic failed:%v", err.Error())
	}
	pair := strings.SplitN(string(payload), ":", 2)
	if len(pair) != 2 {
		return nil, nil, fmt.Errorf("invalid user and password")
	}

	username := pair[0]
	if len(username) == 0 {
		return nil, nil, fmt.Errorf("invalid user")
	}

	password := pair[1]
	if len(password) == 0 {
		return nil, nil, fmt.Errorf("invalid password")
	}

	return []byte(username), []byte(password), nil

}
