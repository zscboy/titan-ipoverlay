package ws

import (
	"fmt"
	"io"
	"net"
	"time"

	"github.com/zeromicro/go-zero/core/logx"
)

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

	startTime := time.Now()

	_, err := proxy.conn.Write(data)
	if err != nil {
		return err
	}

	proxy.tunnel.addTrafficStats(len(data), time.Now().Sub(startTime))

	return nil
}

func (proxy *TCPProxy) proxyConn() error {
	conn := proxy.conn

	buf := make([]byte, 32*1024)
	for {
		n, err := conn.Read(buf)
		if err != nil {
			// 检查是否是 EOF (SOCKS5 关闭写方向)
			if err == io.EOF {
				logx.Infof("session %s: SOCKS5 client closed write direction (EOF)", proxy.id)

				// 1. 通知 Client 发送 HALF_CLOSE
				proxy.tunnel.onProxyTCPConnHalfClose(proxy.id)

				// 2. 关闭读方向（不再读取 SOCKS5）
				if tcpConn, ok := conn.(*net.TCPConn); ok {
					if err := tcpConn.CloseRead(); err != nil {
						logx.Errorf("session %s CloseRead failed: %v", proxy.id, err)
					} else {
						logx.Infof("session %s: closed read direction, write still open", proxy.id)
					}
				}

				// 3. 退出读循环，但保持写方向（可能还需要发送数据）
				return nil
			}

			// 其他错误：完全关闭
			logx.Infof("proxy.proxyConn error: %v", err)
			if !proxy.isCloseByClient {
				proxy.tunnel.onProxyTCPConnClose(proxy.id)
			}
			conn.Close()
			return nil
		}

		// 正常数据，转发
		proxy.tunnel.onProxyDataFromProxy(proxy.id, buf[:n])
	}
}
