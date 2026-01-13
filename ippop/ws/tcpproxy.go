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
	perfStats       *SessionPerfStats // 性能统计
}

func newTCPProxy(id string, conn net.Conn, t *Tunnel, userName string) *TCPProxy {
	return &TCPProxy{
		id:         id,
		conn:       conn,
		tunnel:     t,
		userName:   userName,
		perfStats:  NewSessionPerfStats(id, userName, &t.tunMgr.config.PerfMonitoring),
		activeTime: time.Now(),
		done:       make(chan struct{}),
	}
}

func (proxy *TCPProxy) close() {
	proxy.closeOnce.Do(func() {
		if proxy.conn != nil {
			proxy.conn.Close()
		}

		if proxy.perfStats != nil {
			proxy.perfStats.Close()
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

	writeDuration := time.Since(startTime)

	// T3: POP → Target 写入统计
	if proxy.perfStats != nil {
		proxy.perfStats.AddT3Write(int64(len(data)), writeDuration)
	}

	// Tunnel 级别的统计（保留原有逻辑）
	proxy.tunnel.addTrafficStats(len(data), writeDuration)

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
		// T1: Client → POP 读取
		t1Start := time.Now()
		n, err := conn.Read(buf)
		t1Duration := time.Since(t1Start)

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

		// 记录 T1 统计
		if proxy.perfStats != nil {
			proxy.perfStats.AddT1Read(int64(n), t1Duration)
		}

		// T2: 内部处理（转发数据）
		t2Start := time.Now()
		// proxy.tunnel.tunMgr.traffic(proxy.userName, int64(n))
		proxy.tunnel.onProxyDataFromProxy(proxy.id, buf[:n])
		t2Duration := time.Since(t2Start)

		// 记录 T2 统计
		if proxy.perfStats != nil {
			proxy.perfStats.AddT2Process(t2Duration)
		}
	}
}
