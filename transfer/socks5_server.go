package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/zeromicro/go-zero/core/logx"
)

const (
	clientHandshakeTimeout = 10 * time.Second
	clientReplyTimeout     = 5 * time.Second
)

type Socks5Server struct {
	listenAddr string
	router     *Router
	limiter    *LimiterManager
	listener   *net.TCPListener
	ctx        context.Context
	cancel     context.CancelFunc
	mu         sync.Mutex
	conns      map[net.Conn]struct{}
	stopped    bool
}

func NewSocks5Server(listenAddr string, router *Router, limiter *LimiterManager) *Socks5Server {
	ctx, cancel := context.WithCancel(context.Background())
	return &Socks5Server{
		listenAddr: listenAddr,
		router:     router,
		limiter:    limiter,
		ctx:        ctx,
		cancel:     cancel,
		conns:      make(map[net.Conn]struct{}),
	}
}

func (s *Socks5Server) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped || s.listener != nil {
		return fmt.Errorf("SOCKS5 server has already been started or stopped")
	}
	if s.router == nil || !s.router.HasBackendProtocol("socks5") {
		return fmt.Errorf("SOCKS5 listener requires at least one socks5:// backend")
	}
	addr, err := net.ResolveTCPAddr("tcp", s.listenAddr)
	if err != nil {
		return err
	}
	listener, err := net.ListenTCP("tcp", addr)
	if err != nil {
		return err
	}
	s.listener = listener
	logx.Infof("SOCKS5 Server listening on %s", listener.Addr())
	go s.acceptLoop(listener)
	return nil
}

func (s *Socks5Server) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stopped = true
	s.cancel()
	if s.listener != nil {
		s.listener.Close()
	}
	for conn := range s.conns {
		conn.Close()
	}
}

func (s *Socks5Server) trackConn(conn net.Conn) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped {
		conn.Close()
		return false
	}
	s.conns[conn] = struct{}{}
	return true
}

func (s *Socks5Server) releaseConn(conn net.Conn) {
	conn.Close()
	s.mu.Lock()
	delete(s.conns, conn)
	s.mu.Unlock()
}

func (s *Socks5Server) acceptLoop(listener *net.TCPListener) {
	for {
		conn, err := listener.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return
			}
			logx.Errorf("SOCKS5 Accept error: %v", err)
			select {
			case <-s.ctx.Done():
				return
			case <-time.After(100 * time.Millisecond):
				continue
			}
		}
		if !s.trackConn(conn) {
			return
		}
		go s.handleConn(conn)
	}
}

func parseSession(username string) string {
	idx := strings.Index(username, "-session-")
	if idx == -1 {
		return ""
	}
	sessionPart := username[idx+len("-session-"):]
	dashIdx := strings.Index(sessionPart, "-")
	if dashIdx != -1 {
		return sessionPart[:dashIdx]
	}
	return sessionPart
}

func writeSocks5Reply(conn net.Conn, reply []byte) error {
	if err := conn.SetWriteDeadline(time.Now().Add(clientReplyTimeout)); err != nil {
		return err
	}
	_, err := conn.Write(reply)
	return err
}

func readSocks5Auth(reader io.Reader) ([]byte, string, error) {
	var auth bytes.Buffer
	r := io.TeeReader(reader, &auth)
	var header [2]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return nil, "", err
	}
	if header[0] != 1 || header[1] == 0 {
		return nil, "", fmt.Errorf("invalid SOCKS5 authentication header")
	}
	username := make([]byte, int(header[1]))
	if _, err := io.ReadFull(r, username); err != nil {
		return nil, "", err
	}
	var passLen [1]byte
	if _, err := io.ReadFull(r, passLen[:]); err != nil {
		return nil, "", err
	}
	if passLen[0] == 0 {
		return nil, "", fmt.Errorf("empty SOCKS5 password")
	}
	if _, err := io.CopyN(io.Discard, r, int64(passLen[0])); err != nil {
		return nil, "", err
	}
	return auth.Bytes(), string(username), nil
}

// Requests and replies share the same address framing. Preserve the wire bytes.
func readSocks5Packet(reader io.Reader) ([]byte, error) {
	packet := make([]byte, 4)
	if _, err := io.ReadFull(reader, packet); err != nil {
		return nil, err
	}
	if packet[0] != 5 || packet[2] != 0 {
		return nil, fmt.Errorf("invalid SOCKS5 packet header")
	}
	var addrLen int
	switch packet[3] {
	case 1:
		addrLen = 4
	case 4:
		addrLen = 16
	case 3:
		var size [1]byte
		if _, err := io.ReadFull(reader, size[:]); err != nil {
			return nil, err
		}
		packet = append(packet, size[0])
		addrLen = int(size[0])
	default:
		return nil, fmt.Errorf("unsupported SOCKS5 address type: %d", packet[3])
	}
	address := make([]byte, addrLen+2)
	if _, err := io.ReadFull(reader, address); err != nil {
		return nil, err
	}
	return append(packet, address...), nil
}

func (s *Socks5Server) handleConn(conn net.Conn) {
	defer s.releaseConn(conn)
	if err := conn.SetReadDeadline(time.Now().Add(clientHandshakeTimeout)); err != nil {
		return
	}
	reader := bufio.NewReader(conn)
	var header [2]byte
	if _, err := io.ReadFull(reader, header[:]); err != nil || header[0] != 5 {
		return
	}
	methods := make([]byte, int(header[1]))
	if _, err := io.ReadFull(reader, methods); err != nil {
		return
	}
	method := byte(0xff)
	if bytes.Contains(methods, []byte{2}) {
		method = 2
	} else if bytes.Contains(methods, []byte{0}) {
		method = 0
	}
	if method == 0xff {
		writeSocks5Reply(conn, []byte{5, 0xff})
		return
	}

	var auth []byte
	username := ""
	if method == 2 {
		// Session routing requires the username before a POP can be selected.
		if err := writeSocks5Reply(conn, []byte{5, 2}); err != nil {
			return
		}
		var err error
		auth, username, err = readSocks5Auth(reader)
		if err != nil {
			writeSocks5Reply(conn, []byte{1, 1})
			return
		}
	}
	failure := []byte{5, 0xff}
	if method == 2 {
		failure = []byte{1, 1}
	}
	if s.router == nil {
		writeSocks5Reply(conn, failure)
		return
	}
	backend, err := s.router.SelectBackendForProtocol(username, parseSession(username), "socks5")
	if err != nil {
		logx.Errorf("SOCKS5 select backend failed: %v", err)
		writeSocks5Reply(conn, failure)
		return
	}
	dialer := net.Dialer{Timeout: backendDialTimeout}
	backendConn, err := dialer.DialContext(s.ctx, "tcp", backend.Addr)
	if err != nil {
		logx.Errorf("SOCKS5 dial POP %s failed: %v", backend.Addr, err)
		writeSocks5Reply(conn, failure)
		return
	}
	if !s.trackConn(backendConn) {
		return
	}
	defer s.releaseConn(backendConn)
	backendReader := bufio.NewReader(backendConn)
	if err := backendConn.SetDeadline(time.Now().Add(backendHandshakeTimeout)); err != nil {
		return
	}
	if err := negotiateSocks5POP(backendConn, backendReader, method, auth, conn); err != nil {
		logx.Errorf("SOCKS5 negotiate POP %s failed: %v", backend.Addr, err)
		return
	}
	if err := conn.SetReadDeadline(time.Now().Add(clientHandshakeTimeout)); err != nil {
		return
	}
	request, err := readSocks5Packet(reader)
	if err != nil {
		return
	}
	if request[1] != 1 {
		writeSocks5Reply(conn, []byte{5, 7, 0, 1, 0, 0, 0, 0, 0, 0})
		return
	}
	if err := backendConn.SetDeadline(time.Now().Add(backendHandshakeTimeout)); err != nil {
		return
	}
	if _, err := backendConn.Write(request); err != nil {
		writeSocks5Reply(conn, []byte{5, 1, 0, 1, 0, 0, 0, 0, 0, 0})
		return
	}
	reply, err := readSocks5Packet(backendReader)
	if err != nil {
		logx.Errorf("SOCKS5 CONNECT reply from POP %s failed: %v", backend.Addr, err)
		writeSocks5Reply(conn, []byte{5, 1, 0, 1, 0, 0, 0, 0, 0, 0})
		return
	}
	if err := writeSocks5Reply(conn, reply); err != nil || reply[1] != 0 {
		return
	}
	if err := conn.SetDeadline(time.Time{}); err != nil {
		return
	}
	if err := backendConn.SetDeadline(time.Time{}); err != nil {
		return
	}
	stream := s.limiter.WrapConn(&bufferedConn{Conn: backendConn, reader: backendReader})
	relayTCP(conn, backendConn, reader, stream)
}

func negotiateSocks5POP(backend net.Conn, reader io.Reader, method byte, auth []byte, client net.Conn) error {
	failure := []byte{5, 0xff}
	if method == 2 {
		failure = []byte{1, 1}
	}
	fail := func(err error) error {
		writeSocks5Reply(client, failure)
		return err
	}
	// Offer only the method selected for this client; never downgrade authentication.
	if _, err := backend.Write([]byte{5, 1, method}); err != nil {
		return fail(err)
	}
	var reply [2]byte
	if _, err := io.ReadFull(reader, reply[:]); err != nil {
		return fail(err)
	}
	if reply != [2]byte{5, method} {
		return fail(fmt.Errorf("POP rejected authentication method %d", method))
	}
	if method == 0 {
		return writeSocks5Reply(client, reply[:])
	}
	if _, err := backend.Write(auth); err != nil {
		return fail(err)
	}
	if _, err := io.ReadFull(reader, reply[:]); err != nil {
		return fail(err)
	}
	if reply[0] != 1 {
		return fail(fmt.Errorf("invalid POP authentication reply version"))
	}
	if err := writeSocks5Reply(client, reply[:]); err != nil {
		return err
	}
	if reply[1] != 0 {
		return fmt.Errorf("POP rejected credentials with status %d", reply[1])
	}
	return nil
}
