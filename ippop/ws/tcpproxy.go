package ws

import (
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/zeromicro/go-zero/core/logx"
)

type TCPProxy struct {
	id              string
	conn            net.Conn
	tunnel          *Tunnel
	userName        string
	targetDomain    string // 目标域名
	isCloseByClient bool
	activeTime      time.Time
	done            chan struct{}
	closeOnce       sync.Once
	perfStats       *SessionPerfStats // 性能统计
	downloadTraffic int64
	uploadTraffic   int64
	userBuckets     *DownloadBucketStats
}

func newTCPProxy(id string, conn net.Conn, t *Tunnel, userName, targetDomain, countryCode string) *TCPProxy {
	return &TCPProxy{
		id:           id,
		conn:         conn,
		tunnel:       t,
		userName:     userName,
		targetDomain: targetDomain,
		activeTime:   time.Now(),
		done:         make(chan struct{}),
		perfStats:    NewSessionPerfStats(id, userName, targetDomain, countryCode, t.tunMgr.config.GetNodeID(), t.opts.Id, &t.tunMgr.config.PerfMonitoring, &t.tunMgr.config.QoS, t.tunMgr.perfCollector),
		userBuckets:  t.getOrCreateUserBuckets(userName),
	}
}

func (proxy *TCPProxy) close() {
	proxy.closeOnce.Do(func() {
		if proxy.conn != nil {
			proxy.conn.Close()
		}

		if proxy.perfStats != nil {
			// QoS 结算：三振出局制（仅在开启带宽黑名单时执行）
			if proxy.tunnel.tunMgr.config.QoS.EnableBandwidthBlacklist {
				t3Bytes := proxy.perfStats.T3BytesSent.Load()
				t3Dur := time.Duration(proxy.perfStats.T3Duration.Load()).Seconds()
				if t3Dur > 0 {
					speedMBps := (float64(t3Bytes) / t3Dur) / 1024 / 1024
					redlineMBps := float64(proxy.tunnel.tunMgr.config.QoS.RedlineSpeedKbps) / 1024.0

					// 仅对超过 10KB 的会话进行打分，防止极小流量导致的误差
					if t3Bytes > 10*1024 {
						if speedMBps < redlineMBps {
							proxy.tunnel.tunMgr.AddStrike(proxy.tunnel.opts.Id, proxy.tunnel.opts.IP,
								fmt.Sprintf("Session slow: %.2fKBps < %vKBps", speedMBps*1024, proxy.tunnel.tunMgr.config.QoS.RedlineSpeedKbps))
						} else {
							// 表现良好，清空以往扣分
							proxy.tunnel.tunMgr.ClearStrike(proxy.tunnel.opts.Id)
						}
					}
				}
			}

			proxy.perfStats.Close()
		}

		proxy.tunnel.addUserTrafficStats(proxy.userName, proxy.downloadTraffic, proxy.uploadTraffic)
		proxy.reportDownloadBucket(proxy.uploadTraffic)
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

func (proxy *TCPProxy) write(data []byte, t2StartTime time.Time) error {
	if proxy.conn == nil {
		return fmt.Errorf("session %s conn == nil", proxy.id)
	}

	// T3 开始时间（准备写入 SOCKS5 用户）
	t3StartTime := time.Now()
	proxy.activeTime = t3StartTime

	proxy.uploadTraffic += int64(len(data))

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
		// 从 SOCKS5 用户读取数据
		n, err := conn.Read(buf)

		if err != nil {
			if err == io.EOF && proxy.tunnel.isNodeVersionGreatThanV100() {
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

		// 转发数据到 WebSocket（发送给 Client）
		proxy.tunnel.onProxyDataFromProxy(proxy.id, buf[:n])
		proxy.perfStats.AddT4Read(int64(n))
		proxy.downloadTraffic += int64(n)
	}
}

func (proxy *TCPProxy) GetPerfStats() *SessionPerfStats {
	return proxy.perfStats
}

func (proxy *TCPProxy) reportDownloadBucket(size int64) {
	if proxy.userBuckets == nil {
		return
	}

	const MB = 1024 * 1024
	switch {
	case size <= 1*MB:
		atomic.AddInt64(&proxy.userBuckets.Count1M, 1)
	case size <= 5*MB:
		atomic.AddInt64(&proxy.userBuckets.Count5M, 1)
	case size <= 10*MB:
		atomic.AddInt64(&proxy.userBuckets.Count10M, 1)
	case size <= 50*MB:
		atomic.AddInt64(&proxy.userBuckets.Count50M, 1)
	case size <= 100*MB:
		atomic.AddInt64(&proxy.userBuckets.Count100M, 1)
	case size <= 250*MB:
		atomic.AddInt64(&proxy.userBuckets.Count250M, 1)
	case size <= 500*MB:
		atomic.AddInt64(&proxy.userBuckets.Count500M, 1)
	case size <= 1000*MB:
		atomic.AddInt64(&proxy.userBuckets.Count1000M, 1)
	default:
		atomic.AddInt64(&proxy.userBuckets.CountMore, 1)
	}
}
