package ws

import (
	"fmt"
	"net"
	"time"
	"titan-ipoverlay/ippop/businesspack"
	"titan-ipoverlay/ippop/socks5"
	"titan-ipoverlay/ippop/ws/pb"

	"sync"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/protobuf/proto"
)

type UDPProxy struct {
	id         string
	conn       socks5.UDPConn
	udpInfo    *socks5.Socks5UDPInfo
	activeTime time.Time
	// timeout
	timeout int

	tunnel    *Tunnel
	done      chan struct{}
	once      sync.Once
	perfStats *SessionPerfStats // 性能统计
}

func newProxyUDP(id string, conn socks5.UDPConn, udpInfo *socks5.Socks5UDPInfo, t *Tunnel, timeout int) *UDPProxy {
	return &UDPProxy{
		id:         id,
		conn:       conn,
		udpInfo:    udpInfo,
		tunnel:     t,
		activeTime: time.Now(),
		timeout:    timeout,
		done:       make(chan struct{}),
		perfStats:  NewSessionPerfStats(id, udpInfo.UserName, udpInfo.Dest, businesspack.PackGeneralWeb, t.opts.IP, t.opts.CountryCode, t.tunMgr.perfCollector.nodeID, t.opts.Id, &t.tunMgr.config.PerfMonitoring, &t.tunMgr.config.QoS, t.tunMgr.perfCollector),
	}
}

func (proxy *UDPProxy) writeToSrc(data []byte) error {
	proxy.activeTime = time.Now()
	// proxy.tunnel.tunMgr.traffic(proxy.udpInfo.UserName, int64(len(data)))

	// UDP 下载统计：视为 T1 (Client -> POP via WS) 的一部分（暂定）
	// 注意：UDP 转发逻辑略有不同，这里主要为了统计流量
	if proxy.perfStats != nil {
		proxy.perfStats.AddT1Read(int64(len(data)), 0)
	}

	srcAddr, err := net.ResolveUDPAddr("udp", proxy.udpInfo.Src)
	if err != nil {
		logx.Errorf("UDPProxy %s writeToSrc ResolveUDPAddr failed: %v", proxy.id, err)
		return err
	}

	datagram, err := socks5.NewDatagram(proxy.udpInfo.Dest, data)
	if err != nil {
		logx.Errorf("UDPProxy %s writeToSrc NewDatagram failed: %v", proxy.id, err)
		return err
	}

	_, err = proxy.conn.WriteToUDP(datagram.Bytes(), srcAddr)
	return err
}

func (proxy *UDPProxy) writeToDest(data []byte) error {
	proxy.activeTime = time.Now()
	// proxy.tunnel.tunMgr.traffic(proxy.udpInfo.UserName, int64(len(data)))

	// UDP 上传统计：视为 T4 (User -> POP via SOCKS5)
	if proxy.perfStats != nil {
		proxy.perfStats.AddT4Read(int64(len(data)))
	}

	udpData := pb.UDPData{Addr: proxy.udpInfo.Dest, Data: data}
	payload, err := proto.Marshal(&udpData)
	if err != nil {
		return err
	}

	msg := &pb.Message{}
	msg.Type = pb.MessageType_PROXY_UDP_DATA
	msg.SessionId = proxy.id
	msg.Payload = payload

	buf, err := proto.Marshal(msg)
	if err != nil {
		logx.Errorf("UDPProxy %s onProxyData proto message failed:%s", proxy.id, err.Error())
		return err
	}

	return proxy.tunnel.write(buf)
}

func (proxy *UDPProxy) stop() {
	proxy.once.Do(func() {
		close(proxy.done)
		if proxy.perfStats != nil {
			// QoS 结算：三振出局制（仅在开启带宽黑名单时执行）
			if proxy.tunnel.tunMgr.config.QoS.EnableBandwidthBlacklist {
				t3Bytes := proxy.perfStats.T3BytesSent.Load()
				t3Dur := time.Duration(proxy.perfStats.T3Duration.Load()).Seconds()
				if t3Dur > 0 {
					speedMBps := (float64(t3Bytes) / t3Dur) / 1024 / 1024
					redlineMBps := float64(proxy.tunnel.tunMgr.config.QoS.RedlineSpeedKbps) / 1024.0

					// 仅对有一定流量的 UDP 会话进行打分
					if t3Bytes > 2*1024 {
						if speedMBps < redlineMBps {
							proxy.tunnel.tunMgr.AddStrike(proxy.tunnel.opts.Id, proxy.tunnel.opts.IP,
								fmt.Sprintf("UDP Session slow: %.2fKBps < %vKBps", speedMBps*1024, proxy.tunnel.tunMgr.config.QoS.RedlineSpeedKbps))
						} else {
							proxy.tunnel.tunMgr.ClearStrike(proxy.tunnel.opts.Id)
						}
					}
				}
			}
			proxy.perfStats.Close()

			// UDP 手动上报结束 (因为 Close 不再自动 Dec)
			if proxy.tunnel.tunMgr.perfCollector != nil {
				proxy.tunnel.tunMgr.perfCollector.ReportSessionEnd(proxy.udpInfo.UserName)
			}
		}
	})
}

func (proxy *UDPProxy) waitTimeout() {
	defer proxy.tunnel.proxys.Delete(proxy.id)
	for {
		select {
		case <-proxy.done:
			return
		case <-time.After(10 * time.Second):
			timeout := time.Since(proxy.activeTime)
			if timeout.Seconds() > float64(proxy.timeout) {
				logx.Debugf("UDPProxy %s timeout %f, will delete it", proxy.id, timeout.Seconds())
				return
			}
		}
	}
}

func (proxy *UDPProxy) GetPerfStats() *SessionPerfStats {
	return proxy.perfStats
}
