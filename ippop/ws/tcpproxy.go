package ws

import (
	"fmt"
	"net"

	"sync"

	"github.com/zeromicro/go-zero/core/logx"
)

var bufferPool = sync.Pool{
	New: func() interface{} {
		return make([]byte, 32*1024) // 32KB buffer
	},
}

type TCPProxy struct {
	id              string
	conn            net.Conn
	tunnel          *Tunnel
	userName        string
	isCloseByClient bool
}

func newTCPProxy(id string, conn net.Conn, t *Tunnel, userName string) *TCPProxy {
	return &TCPProxy{id: id, conn: conn, tunnel: t, userName: userName}
}

func (proxy *TCPProxy) close() {
	if proxy.conn == nil {
		logx.Errorf("session %s conn == nil", proxy.id)
		return
	}

	proxy.conn.Close()
}

func (proxy *TCPProxy) closeByClient() {
	proxy.isCloseByClient = true
	proxy.close()
}

func (proxy *TCPProxy) write(data []byte) error {
	if proxy.conn == nil {
		return fmt.Errorf("session %s conn == nil", proxy.id)
	}

	proxy.tunnel.tunMgr.traffic(proxy.userName, int64(len(data)))
	_, err := proxy.conn.Write(data)
	return err
}

func (proxy *TCPProxy) proxyConn() error {
	conn := proxy.conn
	defer conn.Close()

	buf := bufferPool.Get().([]byte)
	defer bufferPool.Put(buf)

	for {
		n, err := conn.Read(buf)
		if err != nil {
			if proxy.isCloseByClient {
				logx.Debugf("[BROWSER_INFO] Browser closed SOCKS5 connection (Normal). id: %s", proxy.id)
			} else {
				logx.Infof("[NODE_UNSTABLE] SOCKS5 connection broken by local side: %v, id: %s", err, proxy.id)
				proxy.tunnel.onProxyTCPConnClose(proxy.id)
			}
			return nil
		}

		proxy.tunnel.tunMgr.traffic(proxy.userName, int64(n))
		proxy.tunnel.onProxyDataFromProxy(proxy.id, buf[:n])
	}
}
