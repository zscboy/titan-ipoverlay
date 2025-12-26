package tunnel

import (
	"fmt"
	"net"
	"time"

	"github.com/zeromicro/go-zero/core/logx"
)

type UDPProxy struct {
	id      string
	conn    *net.UDPConn
	timeout int
}

func (proxy *UDPProxy) destroy() {
	if proxy.conn != nil {
		proxy.conn.Close()
		proxy.conn = nil
	}
}

func (proxy *UDPProxy) write(data []byte) error {
	if proxy.conn == nil {
		return fmt.Errorf("session %s conn == nil", proxy.id)
	}

	if err := proxy.conn.SetDeadline(time.Now().Add(time.Duration(proxy.timeout) * time.Second)); err != nil {
		logx.Errorf("UDPProxy.write SetDeadline failed:%v", err)
		return err
	}

	_, err := proxy.conn.Write(data)
	return err
}

func (proxy *UDPProxy) serve(t *Tunnel) error {
	defer t.proxyUDPs.Delete(proxy.id)

	conn := proxy.conn
	defer conn.Close()

	buf := bufferPool.Get().([]byte)
	defer bufferPool.Put(buf)

	for {
		if err := conn.SetDeadline(time.Now().Add(time.Duration(proxy.timeout) * time.Second)); err != nil {
			return fmt.Errorf("UDPProxy.serve SetDeadline failed:%v", err)
		}

		n, _, err := conn.ReadFromUDP(buf)
		if err != nil {
			logx.Debugf("UDPProxy.serve ReadFromUDP: %v", err)
			return nil
		}
		t.onProxyUdpDataFromProxy(proxy.id, buf[:n])
	}
}
