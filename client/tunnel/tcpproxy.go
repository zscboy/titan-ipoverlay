package tunnel

import (
	"fmt"
	"net"

	"sync"

	"github.com/zeromicro/go-zero/core/logx"
)

var bufferPool = sync.Pool{
	New: func() interface{} {
		return make([]byte, 32*1024)
	},
}

type TCPProxy struct {
	id              string
	conn            net.Conn
	isCloseByServer bool
	writeQueue      chan []byte
	connected       chan struct{}
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
	dataCopy := make([]byte, len(data))
	copy(dataCopy, data)

	select {
	case proxy.writeQueue <- dataCopy:
		return nil
	default:
		logx.Errorf("session %s writeQueue full", proxy.id)
		return fmt.Errorf("session %s writeQueue full", proxy.id)
	}
}

func (proxy *TCPProxy) setConn(conn net.Conn) {
	proxy.conn = conn
	close(proxy.connected)
}

func (proxy *TCPProxy) startWriteLoop() {
	<-proxy.connected
	for data := range proxy.writeQueue {
		if proxy.conn == nil {
			return
		}
		_, err := proxy.conn.Write(data)
		if err != nil {
			logx.Errorf("session %s write error: %v", proxy.id, err)
			return
		}
	}
}

func (proxy *TCPProxy) proxyConn(t *Tunnel) {
	defer t.proxySessions.Delete(proxy.id)
	defer close(proxy.writeQueue)

	go proxy.startWriteLoop()

	<-proxy.connected
	conn := proxy.conn
	defer conn.Close()

	buf := bufferPool.Get().([]byte)
	defer bufferPool.Put(buf)

	for {
		n, err := conn.Read(buf)
		if err != nil {
			logx.Debugf("TCPProxy.proxyConn: %s, id: %s", err.Error(), proxy.id)
			if !proxy.isCloseByServer {
				t.onProxyConnClose(proxy.id)
			}
			return
		}
		t.onProxySessionDataFromProxy(proxy.id, buf[:n])
	}
}
