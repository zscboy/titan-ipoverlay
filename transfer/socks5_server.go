package main

import (
	"bufio"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"strings"

	"github.com/zeromicro/go-zero/core/logx"
)

type Socks5Server struct {
	listenAddr string
	router     *Router
	limiter    *LimiterManager
	listener   *net.TCPListener
}

func NewSocks5Server(listenAddr string, router *Router, limiter *LimiterManager) *Socks5Server {
	return &Socks5Server{
		listenAddr: listenAddr,
		router:     router,
		limiter:    limiter,
	}
}

func (s *Socks5Server) Start() error {
	addr, err := net.ResolveTCPAddr("tcp", s.listenAddr)
	if err != nil {
		return err
	}

	listener, err := net.ListenTCP("tcp", addr)
	if err != nil {
		return err
	}
	s.listener = listener

	logx.Infof("SOCKS5 Server listening on %s", s.listenAddr)
	go s.acceptLoop()
	return nil
}

func (s *Socks5Server) Stop() {
	if s.listener != nil {
		s.listener.Close()
	}
}

func (s *Socks5Server) acceptLoop() {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			if strings.Contains(err.Error(), "use of closed network connection") {
				return
			}
			errStr := err.Error()
			if strings.Contains(errStr, "too many open files") {
				logx.Errorf("CRITICAL: File descriptor limit reached (too many open files) on SOCKS5 Accept: %v", err)
			} else {
				logx.Errorf("SOCKS5 Accept error: %v", err)
			}
			continue
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

func (s *Socks5Server) handleConn(conn net.Conn) {
	defer conn.Close()
	reader := bufio.NewReader(conn)

	// 1. Read version and auth methods
	versionAndMethods := make([]byte, 2)
	if _, err := io.ReadAtLeast(reader, versionAndMethods, 2); err != nil {
		logx.Errorf("SOCKS5 read handshake version error: %v", err)
		return
	}

	if versionAndMethods[0] != 0x05 {
		logx.Errorf("SOCKS5 unsupported version: %d", versionAndMethods[0])
		return
	}

	numMethods := int(versionAndMethods[1])
	methods := make([]byte, numMethods)
	if _, err := io.ReadAtLeast(reader, methods, numMethods); err != nil {
		logx.Errorf("SOCKS5 read auth methods error: %v", err)
		return
	}

	// 2. Select auth method (Support Username/Password auth or No Auth)
	hasUserPass := false
	hasNoAuth := false
	for _, m := range methods {
		if m == 0x02 {
			hasUserPass = true
		} else if m == 0x00 {
			hasNoAuth = true
		}
	}

	selectedMethod := byte(0xff) // No acceptable methods
	if hasUserPass {
		selectedMethod = 0x02
	} else if hasNoAuth {
		selectedMethod = 0x00
	}

	if _, err := conn.Write([]byte{0x05, selectedMethod}); err != nil {
		logx.Errorf("SOCKS5 write auth method response error: %v", err)
		return
	}

	if selectedMethod == 0xff {
		logx.Errorf("SOCKS5 no acceptable auth methods found")
		return
	}

	session := ""
	clientUsername := ""
	clientPassword := ""

	// 3. Perform Authentication if necessary
	if selectedMethod == 0x02 {
		// Read Username/Password auth request
		authHeader := make([]byte, 2)
		if _, err := io.ReadAtLeast(reader, authHeader, 2); err != nil {
			logx.Errorf("SOCKS5 auth header read error: %v", err)
			return
		}
		if authHeader[0] != 0x01 { // Auth version 1
			logx.Errorf("SOCKS5 auth version mismatch: %d", authHeader[0])
			return
		}

		userLen := int(authHeader[1])
		userBuf := make([]byte, userLen)
		if _, err := io.ReadAtLeast(reader, userBuf, userLen); err != nil {
			logx.Errorf("SOCKS5 auth read username error: %v", err)
			return
		}
		username := string(userBuf)
		session = parseSession(username)
		clientUsername = username

		passLenBuf := []byte{0}
		if _, err := reader.Read(passLenBuf); err != nil {
			logx.Errorf("SOCKS5 auth read password len error: %v", err)
			return
		}
		passLen := int(passLenBuf[0])
		passBuf := make([]byte, passLen)
		if _, err := io.ReadAtLeast(reader, passBuf, passLen); err != nil {
			logx.Errorf("SOCKS5 auth read password error: %v", err)
			return
		}
		clientPassword = string(passBuf)

		// Accept credentials (always success, but extract session)
		if _, err := conn.Write([]byte{0x01, 0x00}); err != nil {
			logx.Errorf("SOCKS5 write auth success reply error: %v", err)
			return
		}
	}

	// 4. Read client CONNECT request
	reqHeader := make([]byte, 4)
	if _, err := io.ReadAtLeast(reader, reqHeader, 4); err != nil {
		logx.Errorf("SOCKS5 read CONNECT header error: %v", err)
		return
	}

	if reqHeader[0] != 0x05 || reqHeader[1] != 0x01 { // Only support SOCKS5 CONNECT
		logx.Errorf("SOCKS5 unsupported command: version=%d cmd=%d", reqHeader[0], reqHeader[1])
		return
	}

	var destHost string
	var destPort int

	switch reqHeader[3] {
	case 0x01: // IPv4
		ipBuf := make([]byte, 4)
		if _, err := io.ReadAtLeast(reader, ipBuf, 4); err != nil {
			logx.Errorf("SOCKS5 read IPv4 error: %v", err)
			return
		}
		destHost = net.IP(ipBuf).String()
	case 0x04: // IPv6
		ipBuf := make([]byte, 16)
		if _, err := io.ReadAtLeast(reader, ipBuf, 16); err != nil {
			logx.Errorf("SOCKS5 read IPv6 error: %v", err)
			return
		}
		destHost = net.IP(ipBuf).String()
	case 0x03: // FQDN
		lenBuf := []byte{0}
		if _, err := reader.Read(lenBuf); err != nil {
			logx.Errorf("SOCKS5 read FQDN len error: %v", err)
			return
		}
		fqdnLen := int(lenBuf[0])
		fqdnBuf := make([]byte, fqdnLen)
		if _, err := io.ReadAtLeast(reader, fqdnBuf, fqdnLen); err != nil {
			logx.Errorf("SOCKS5 read FQDN name error: %v", err)
			return
		}
		destHost = string(fqdnBuf)
	default:
		logx.Errorf("SOCKS5 unsupported address type: %d", reqHeader[3])
		return
	}

	portBuf := make([]byte, 2)
	if _, err := io.ReadAtLeast(reader, portBuf, 2); err != nil {
		logx.Errorf("SOCKS5 read port error: %v", err)
		return
	}
	destPort = int(binary.BigEndian.Uint16(portBuf))

	// 5. Select Backend via Router
	backend, err := s.router.SelectBackend(clientUsername, session)
	if err != nil {
		logx.Errorf("SOCKS5 select backend failed: %v", err)
		return
	}

	// 6. Connect to Backend
	backendConn, err := ConnectBackend(backend, destHost, destPort, clientUsername, clientPassword)
	if err != nil {
		logx.Errorf("SOCKS5 connect to backend %s://%s failed: %v", backend.Type, backend.Addr, err)
		// Reply with failure code 0x04 (Host unreachable)
		conn.Write([]byte{0x05, 0x04, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00})
		return
	}
	backendConn = s.limiter.WrapConn(backendConn)
	defer backendConn.Close()

	// 7. Reply Success to SOCKS5 client
	// Reply format: version | success (0x00) | reserved (0x00) | IPv4 | 0.0.0.0 | port 0
	if _, err := conn.Write([]byte{0x05, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}); err != nil {
		logx.Errorf("SOCKS5 reply success to client failed: %v", err)
		return
	}

	// 8. Bidirectional copy
	go func() {
		_, relayErr := io.Copy(backendConn, reader)
		if relayErr != nil && !errors.Is(relayErr, net.ErrClosed) {
			logx.Errorf("[Downstream Connection Drop] SOCKS5 target %s:%d: %v", destHost, destPort, relayErr)
		}
		backendConn.Close()
		conn.Close()
	}()
	_, relayErr := io.Copy(conn, backendConn)
	if relayErr != nil && !errors.Is(relayErr, net.ErrClosed) {
		logx.Errorf("[1007 Upstream Connection Drop] SOCKS5 target %s:%d: %v", destHost, destPort, relayErr)
	}
	conn.Close()
	backendConn.Close()
}
