package socks5

import (
	"errors"
	"fmt"
	"net"

	"github.com/google/uuid"
	"github.com/zeromicro/go-zero/core/logx"
)

type UDPServer struct {
	port   int
	conn   *net.UDPConn
	server *Socks5Server
	id     string
	user   string
}

func newUDPServer(startPort, endPort int, server *Socks5Server, user string) (*UDPServer, error) {
	port, conn, err := listen(startPort, endPort)
	if err != nil {
		return nil, err
	}

	return &UDPServer{port: port, conn: conn, server: server, id: uuid.NewString(), user: user}, nil
}

func listen(startPort, endPort int) (int, *net.UDPConn, error) {
	for port := startPort; port <= endPort; port++ {
		addr := net.UDPAddr{
			IP:   net.IPv4zero,
			Port: port,
		}
		conn, err := net.ListenUDP("udp", &addr)
		if err == nil {
			return port, conn, nil
		}
	}
	return 0, nil, fmt.Errorf("no available UDP ports in range %d-%d", startPort, endPort)

}

func (udp *UDPServer) serve() {
	if udp.conn == nil {
		panic("udp.conn == nil")
	}

	buf := make([]byte, 4096)
	for {
		n, src, err := udp.conn.ReadFromUDP(buf)
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				break
			}

			logx.Error("UDP.serve ReadFromUDP error:", err)
			continue
		}

		udp.handleUDPConn(src, buf[:n])
	}

	logx.Infof("user %s udp server close", udp.user)
}

func (udp *UDPServer) handleUDPConn(src *net.UDPAddr, data []byte) error {
	defer func() {
		if r := recover(); r != nil {
			logx.Errorf("UDP.handleUDPConn Recovered. Error:%s", r)
		}
	}()

	srcIP := src.IP.String()
	keyUserIPCount := fmt.Sprintf("%s:%s", udp.user, srcIP)
	if udp.server.userIPCount.get(keyUserIPCount) <= 0 {
		return fmt.Errorf("user %s ip %s not associate", udp.user, srcIP)
	}

	datagram, err := newDatagramFromBytes(data)
	if err != nil {
		return err
	}

	dest := toAddress(datagram.atyp, datagram.dstAddr, datagram.dstPort)

	udpAddr, err := net.ResolveUDPAddr("udp", dest)
	if err != nil {
		return err
	}

	if udpAddr.IP.IsLoopback() || udpAddr.IP.IsPrivate() || udpAddr.IP.IsMulticast() || udpAddr.IP.IsLinkLocalMulticast() {
		return fmt.Errorf("UDPServer.handleUDPConn not support ip %s", udpAddr.IP.String())
	}

	udpInfo := &Socks5UDPInfo{UDPServerID: udp.id, Src: src.String(), Dest: dest, UserName: udp.user}

	if udp.server == nil || udp.server.opts == nil || udp.server.opts.Handler == nil {
		return fmt.Errorf("UDP.handleUDPConn, handler is nil")
	}

	return udp.server.opts.Handler.HandleSocks5UDP(udp.conn, udpInfo, datagram.data)
}

func (udp *UDPServer) stop() {
	if udp.conn != nil {
		udp.conn.Close()
	}
}
