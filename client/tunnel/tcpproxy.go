package tunnel

import (
	"fmt"
	"net"

	"github.com/zeromicro/go-zero/core/logx"
)

type TCPProxy struct {
	id              string
	conn            net.Conn
	isCloseByServer bool
}

func (proxy *TCPProxy) destroy() {
	proxy.closeByServer()
}

func (proxy *TCPProxy) close() {
	if proxy.conn == nil {
		logx.Errorf("session %s conn == nil", proxy.id)
		return
	}

	proxy.conn.Close()
}

func (proxy *TCPProxy) closeByServer() {
	proxy.isCloseByServer = true
	proxy.close()
}

func (proxy *TCPProxy) write(data []byte) error {
	if proxy.conn == nil {
		return fmt.Errorf("session %s conn == nil", proxy.id)
	}

	_, err := proxy.conn.Write(data)
	return err
}

func (proxy *TCPProxy) proxyConn(t *Tunnel) {
	defer t.proxySessions.Delete(proxy.id)

	conn := proxy.conn
	defer conn.Close()

	buf := make([]byte, 4096)
	for {
		n, err := conn.Read(buf)
		if err != nil {
			logx.Debugf("TCPProxy.proxyConn: %s", err.Error())
			if !proxy.isCloseByServer {
				t.onProxyConnClose(proxy.id)
			}
			return
		}
		t.onProxySessionDataFromProxy(proxy.id, buf[:n])
	}
}
