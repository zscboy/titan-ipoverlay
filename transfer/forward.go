package main

import (
	"bufio"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/zeromicro/go-zero/core/logx"
)

func ConnectBackend(backend BackendInfo, targetHost string, targetPort int, clientUsername, clientPassword string) (net.Conn, error) {
	dialer := net.Dialer{Timeout: 10 * time.Second}
	conn, err := dialer.Dial("tcp", backend.Addr)
	if err != nil {
		errStr := err.Error()
		if strings.Contains(errStr, "too many open files") {
			logx.Errorf("CRITICAL: File descriptor limit reached (too many open files) on ConnectBackend Dial: %v", err)
		} else if strings.Contains(errStr, "cannot assign requested address") || strings.Contains(errStr, "EADDRNOTAVAIL") {
			logx.Errorf("CRITICAL: Local port exhaustion (cannot assign requested address) on ConnectBackend Dial: %v", err)
		}
		return nil, fmt.Errorf("failed to dial backend %s: %v", backend.Addr, err)
	}

	success := false
	defer func() {
		if !success {
			conn.Close()
		}
	}()

	if backend.Type == "socks5" {
		if err := handshakeSocks5(conn, clientUsername, clientPassword, targetHost, targetPort); err != nil {
			return nil, fmt.Errorf("socks5 handshake with backend failed: %v", err)
		}
	} else if backend.Type == "http" {
		if err := handshakeHTTP(conn, clientUsername, clientPassword, targetHost, targetPort); err != nil {
			return nil, fmt.Errorf("http CONNECT handshake with backend failed: %v", err)
		}
	} else {
		return nil, fmt.Errorf("unknown backend type: %s", backend.Type)
	}

	success = true
	return conn, nil
}

func handshakeSocks5(conn net.Conn, clientUsername, clientPassword string, targetHost string, targetPort int) error {
	// 1. Send greeting
	// If clientUsername has username, support No Auth (0x00) and User/Pass (0x02)
	var greeting []byte
	if clientUsername != "" {
		greeting = []byte{0x05, 0x02, 0x00, 0x02}
	} else {
		greeting = []byte{0x05, 0x01, 0x00}
	}

	if _, err := conn.Write(greeting); err != nil {
		return err
	}

	// 2. Read greeting response
	resp := make([]byte, 2)
	if _, err := io.ReadAtLeast(conn, resp, 2); err != nil {
		return err
	}
	if resp[0] != 0x05 {
		return fmt.Errorf("invalid socks version from backend SOCKS5: %d", resp[0])
	}

	// Perform Username/Password auth if selected by the backend
	if resp[1] == 0x02 {
		if clientUsername == "" {
			return fmt.Errorf("backend SOCKS5 requested Username/Password auth but none configured")
		}
		// Send Subnegotiation request
		// version (0x01) | ulen | username | plen | password
		req := []byte{0x01, byte(len(clientUsername))}
		req = append(req, []byte(clientUsername)...)
		req = append(req, byte(len(clientPassword)))
		req = append(req, []byte(clientPassword)...)

		if _, err := conn.Write(req); err != nil {
			return err
		}

		// Read response
		// version (0x01) | status (0x00 is success)
		authResp := make([]byte, 2)
		if _, err := io.ReadAtLeast(conn, authResp, 2); err != nil {
			return err
		}
		if authResp[0] != 0x01 || authResp[1] != 0x00 {
			return fmt.Errorf("authentication failed with backend SOCKS5, status code: %d", authResp[1])
		}
	} else if resp[1] != 0x00 {
		return fmt.Errorf("unacceptable auth method from backend SOCKS5: %d", resp[1])
	}

	// 3. Send SOCKS5 CONNECT request
	// Format: \x05 (version) | \x01 (CONNECT) | \x00 (reserved) | atyp | destAddr | destPort
	var atyp byte
	var addrBody []byte

	ip := net.ParseIP(targetHost)
	if ip != nil {
		if ip4 := ip.To4(); ip4 != nil {
			atyp = 0x01 // IPv4
			addrBody = ip4
		} else if ip6 := ip.To16(); ip6 != nil {
			atyp = 0x04 // IPv6
			addrBody = ip6
		}
	} else {
		atyp = 0x03 // Domain name
		addrBody = append([]byte{byte(len(targetHost))}, []byte(targetHost)...)
	}

	reqHeader := make([]byte, 4+len(addrBody)+2)
	reqHeader[0] = 0x05
	reqHeader[1] = 0x01 // CONNECT
	reqHeader[2] = 0x00 // Reserved
	reqHeader[3] = atyp
	copy(reqHeader[4:], addrBody)
	binary.BigEndian.PutUint16(reqHeader[4+len(addrBody):], uint16(targetPort))

	if _, err := conn.Write(reqHeader); err != nil {
		return err
	}

	// 4. Read SOCKS5 server response
	// Read first 4 bytes of reply: version | reply | reserved | atyp
	replyBuf := make([]byte, 4)
	if _, err := io.ReadAtLeast(conn, replyBuf, 4); err != nil {
		return err
	}

	if replyBuf[0] != 0x05 {
		return fmt.Errorf("invalid reply version from backend SOCKS5: %d", replyBuf[0])
	}
	if replyBuf[1] != 0x00 {
		return fmt.Errorf("backend SOCKS5 failed to connect, reply code: %d", replyBuf[1])
	}

	// Drain address spec in response
	switch replyBuf[3] {
	case 0x01: // IPv4 (4 bytes IP + 2 bytes port)
		drain := make([]byte, 4+2)
		if _, err := io.ReadAtLeast(conn, drain, len(drain)); err != nil {
			return err
		}
	case 0x04: // IPv6 (16 bytes IP + 2 bytes port)
		drain := make([]byte, 16+2)
		if _, err := io.ReadAtLeast(conn, drain, len(drain)); err != nil {
			return err
		}
	case 0x03: // Domain name (1 byte length + length bytes IP + 2 bytes port)
		lenBuf := []byte{0}
		if _, err := conn.Read(lenBuf); err != nil {
			return err
		}
		drain := make([]byte, int(lenBuf[0])+2)
		if _, err := io.ReadAtLeast(conn, drain, len(drain)); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unknown address type in SOCKS5 reply: %d", replyBuf[3])
	}

	return nil
}

func handshakeHTTP(conn net.Conn, clientUsername, clientPassword string, targetHost string, targetPort int) error {
	hostPort := net.JoinHostPort(targetHost, strconv.Itoa(targetPort))
	req := fmt.Sprintf("CONNECT %s HTTP/1.1\r\nHost: %s\r\n", hostPort, hostPort)
	if clientUsername != "" {
		auth := base64.StdEncoding.EncodeToString([]byte(clientUsername + ":" + clientPassword))
		req += fmt.Sprintf("Proxy-Authorization: Basic %s\r\n", auth)
	}
	req += "\r\n"

	if _, err := conn.Write([]byte(req)); err != nil {
		return err
	}

	// Read HTTP response headers line by line until \r\n\r\n
	reader := bufio.NewReader(conn)
	statusLine, err := reader.ReadString('\n')
	if err != nil {
		return err
	}

	// Example: HTTP/1.1 200 Connection Established
	if !strings.Contains(statusLine, " 200 ") {
		return fmt.Errorf("bad response from HTTP proxy: %s", strings.TrimSpace(statusLine))
	}

	// Read remaining headers until blank line
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return err
		}
		if line == "\r\n" || line == "\n" {
			break
		}
	}

	return nil
}

func Relay(conn1, conn2 net.Conn) {
	errChan := make(chan error, 2)
	go func() {
		_, err := io.Copy(conn1, conn2)
		conn1.Close()
		conn2.Close()
		errChan <- err
	}()
	go func() {
		_, err := io.Copy(conn2, conn1)
		conn2.Close()
		conn1.Close()
		errChan <- err
	}()
	<-errChan
}
