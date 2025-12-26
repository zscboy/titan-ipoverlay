package ws

import (
	"net"
	"time"
	"titan-ipoverlay/ippop/socks5"
	"titan-ipoverlay/ippop/ws/pb"

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

	tunnel *Tunnel

	srcAddr *net.UDPAddr
}

func newProxyUDP(id string, conn socks5.UDPConn, udpInfo *socks5.Socks5UDPInfo, t *Tunnel, timeout int) *UDPProxy {
	return &UDPProxy{id: id, conn: conn, udpInfo: udpInfo, tunnel: t, activeTime: time.Now(), timeout: timeout}
}

func (proxy *UDPProxy) writeToSrc(data []byte) error {
	proxy.activeTime = time.Now()
	proxy.tunnel.tunMgr.traffic(proxy.udpInfo.UserName, int64(len(data)))

	if proxy.srcAddr == nil {
		addr, err := net.ResolveUDPAddr("udp", proxy.udpInfo.Src)
		if err != nil {
			return err
		}
		proxy.srcAddr = addr
	}

	datagram, err := socks5.NewDatagram(proxy.udpInfo.Dest, data)
	if err != nil {
		return err
	}

	_, err = proxy.conn.WriteToUDP(datagram.Bytes(), proxy.srcAddr)
	return err
}

func (proxy *UDPProxy) writeToDest(data []byte) error {
	proxy.activeTime = time.Now()
	proxy.tunnel.tunMgr.traffic(proxy.udpInfo.UserName, int64(len(data)))

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
		logx.Errorf("onProxyData proto message failed:%s", err.Error())
		return err
	}

	return proxy.tunnel.writeAsync(buf)
}

func (proxy *UDPProxy) waitTimeout() {
	defer proxy.tunnel.proxys.Delete(proxy.id)
	for {
		time.Sleep(10 * time.Second)

		timeout := time.Since(proxy.activeTime)
		if timeout.Seconds() > float64(proxy.timeout) {
			logx.Debugf("UDPProxy %s timeout %f, will delete it", proxy.id, timeout.Seconds())
			break
		}
	}
}
