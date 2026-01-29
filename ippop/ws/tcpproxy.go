package ws

import (
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"github.com/zeromicro/go-zero/core/logx"
)

type TCPProxy struct {
	id              string
	conn            net.Conn
	tunnel          *Tunnel
	userName        string
	isCloseByClient bool
	activeTime      time.Time
	done            chan struct{}
	closeOnce       sync.Once
}

func newTCPProxy(id string, conn net.Conn, t *Tunnel, userName string) *TCPProxy {
	return &TCPProxy{id: id, conn: conn, tunnel: t, userName: userName, activeTime: time.Now(), done: make(chan struct{})}
}

func (proxy *TCPProxy) close() {
	proxy.closeOnce.Do(func() {
		if proxy.conn != nil {
			proxy.conn.Close()
		}
		close(proxy.done)
	})
}

func (proxy *TCPProxy) closeByClient() {
	proxy.isCloseByClient = true
	proxy.close()
}

func (proxy *TCPProxy) closeWrite() error {
	if proxy.conn == nil {
		logx.Errorf("session %s conn == nil", proxy.id)
		return fmt.Errorf("session %s conn == nil", proxy.id)
	}

	conn := proxy.conn
	if tcpConn, ok := conn.(*net.TCPConn); ok {
		if err := tcpConn.CloseWrite(); err != nil {
			logx.Errorf("session %s CloseWrite failed: %v", proxy.id, err)
			return err
		}
	}

	return nil
}

func (proxy *TCPProxy) write(data []byte) error {
	if proxy.conn == nil {
		return fmt.Errorf("session %s conn == nil", proxy.id)
	}

	startTime := time.Now()
	proxy.activeTime = startTime

	_, err := proxy.conn.Write(data)
	if err != nil {
		return err
	}

	proxy.tunnel.addTrafficStats(len(data), time.Now().Sub(startTime))

	return nil
}

func (proxy *TCPProxy) waitHalfCloseTimeout() {
	timeout := time.Duration(proxy.tunnel.opts.TCPTimeout) * time.Second
	if timeout <= 0 {
		timeout = 3 * time.Minute
	}

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-proxy.done:
			logx.Debugf("proxy waitHalfCloseTimeout exit")
			return
		case <-ticker.C:
			if time.Since(proxy.activeTime) > timeout {
				logx.Errorf("session %s half-close idle timeout %f, will delete it", proxy.id, time.Since(proxy.activeTime).Seconds())
				proxy.tunnel.onProxyTCPConnClose(proxy.id)
				proxy.close()
				return
			}
		}
	}
}

func (proxy *TCPProxy) proxyConn() error {
	conn := proxy.conn

	buf := make([]byte, 32*1024)
	for {
		n, err := conn.Read(buf)
		if err != nil {
			if err == io.EOF && proxy.tunnel.isNodeVersionGreatThanV011() {
				logx.Infof("session %s: SOCKS5 client closed write direction (EOF)", proxy.id)

				proxy.tunnel.onProxyTCPConnHalfClose(proxy.id)
				go proxy.waitHalfCloseTimeout()
				return nil
			}

			logx.Infof("Tunnel %s proxy.proxyConn error user %s session %s: %v", proxy.tunnel.opts.Id, proxy.userName, proxy.id, err)
			if !proxy.isCloseByClient {
				proxy.tunnel.onProxyTCPConnClose(proxy.id)
			}
			proxy.close()
			return nil
		}

		proxy.tunnel.onProxyDataFromProxy(proxy.id, buf[:n])
	}
}
