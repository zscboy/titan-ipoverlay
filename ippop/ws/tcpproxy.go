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
	perfStats       *SessionPerfStats // 性能统计
}

func newTCPProxy(id string, conn net.Conn, t *Tunnel, userName string) *TCPProxy {
	return &TCPProxy{
		id:        id,
		conn:      conn,
		tunnel:    t,
		userName:  userName,
		perfStats: NewSessionPerfStats(id, userName, &t.tunMgr.config.PerfMonitoring, t.tunMgr.perfCollector),
	}
}

func (proxy *TCPProxy) close() {
	if proxy.conn == nil {
		logx.Errorf("session %s conn == nil", proxy.id)
		return
	}

	// 输出性能统计
	if proxy.perfStats != nil {
		proxy.perfStats.Close()
	}

	proxy.conn.Close()
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

func (proxy *TCPProxy) write(data []byte, t2StartTime time.Time) error {
	if proxy.conn == nil {
		return fmt.Errorf("session %s conn == nil", proxy.id)
	}

	// T3 开始时间（准备写入 SOCKS5 用户）
	t3StartTime := time.Now()

	// T2: 内部处理时间（从 WebSocket 读取完成到 SOCKS5 写入开始）
	t2Duration := t3StartTime.Sub(t2StartTime)
	if proxy.perfStats != nil {
		proxy.perfStats.AddT2Process(t2Duration)
	}

	// T3: IPPop → 用户（SOCKS5 写入）
	_, err := proxy.conn.Write(data)
	if err != nil {
		return err
	}

	t3Duration := time.Since(t3StartTime)

	// 记录 T3 统计（IPPop → 用户）
	if proxy.perfStats != nil {
		proxy.perfStats.AddT3Write(int64(len(data)), t3Duration)
	}

	// Tunnel 级别的统计
	proxy.tunnel.addTrafficStats(len(data), t3Duration)

	return nil
}

func (proxy *TCPProxy) proxyConn() error {
	conn := proxy.conn

	buf := make([]byte, 32*1024)
	for {
		// 从 SOCKS5 用户读取数据（这是反向流量，不是 T1）
		n, err := conn.Read(buf)

		if err != nil {
			if err == io.EOF && proxy.tunnel.isNodeVersionGreatThanV011() {
				logx.Infof("session %s: SOCKS5 client closed write direction (EOF)", proxy.id)

				proxy.tunnel.onProxyTCPConnHalfClose(proxy.id)
				return nil
			}

			logx.Infof("proxy.proxyConn error: %v", err)
			if !proxy.isCloseByClient {
				proxy.tunnel.onProxyTCPConnClose(proxy.id)
			}
			conn.Close()
			return nil
		}

		// 转发数据到 WebSocket（发送给 Client）
		proxy.tunnel.onProxyDataFromProxy(proxy.id, buf[:n])
	}
}
