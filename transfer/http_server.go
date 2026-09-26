package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/zeromicro/go-zero/core/logx"
)

const httpHeaderTimeout = 10 * time.Second

var errHTTPHeaderTooLarge = errors.New("HTTP request header too large")

type HTTPServer struct {
	listenAddr string
	router     *Router
	limiter    *LimiterManager
	listener   net.Listener
	ctx        context.Context
	cancel     context.CancelFunc
	mu         sync.Mutex
	conns      map[net.Conn]struct{}
	stopped    bool
}

func NewHTTPServer(listenAddr string, router *Router, limiter *LimiterManager) *HTTPServer {
	ctx, cancel := context.WithCancel(context.Background())
	return &HTTPServer{
		listenAddr: listenAddr,
		router:     router,
		limiter:    limiter,
		ctx:        ctx,
		cancel:     cancel,
		conns:      make(map[net.Conn]struct{}),
	}
}

func (s *HTTPServer) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped || s.listener != nil {
		return fmt.Errorf("HTTP server has already been started or stopped")
	}
	if s.router == nil || !s.router.HasBackendProtocol("http") {
		return fmt.Errorf("HTTP listener requires at least one http:// backend")
	}
	listener, err := net.Listen("tcp", s.listenAddr)
	if err != nil {
		return err
	}
	s.listener = listener
	logx.Infof("HTTP Proxy Server listening on %s", listener.Addr())
	go s.acceptLoop(listener)
	return nil
}

func (s *HTTPServer) Stop() {
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

func (s *HTTPServer) trackConn(conn net.Conn) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped {
		conn.Close()
		return false
	}
	s.conns[conn] = struct{}{}
	return true
}

func (s *HTTPServer) releaseConn(conn net.Conn) {
	conn.Close()
	s.mu.Lock()
	delete(s.conns, conn)
	s.mu.Unlock()
}

func (s *HTTPServer) acceptLoop(listener net.Listener) {
	for {
		conn, err := listener.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return
			}
			logx.Errorf("HTTP Accept error: %v", err)
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

func readHTTPPrefix(conn io.Reader) ([]byte, string, error) {
	var prefix bytes.Buffer
	limited := io.LimitReader(conn, http.DefaultMaxHeaderBytes)
	reader := bufio.NewReader(io.TeeReader(limited, &prefix))
	req, err := http.ReadRequest(reader)
	if err != nil {
		if prefix.Len() == http.DefaultMaxHeaderBytes {
			return nil, "", errHTTPHeaderTooLarge
		}
		return nil, "", err
	}
	username := ""
	scheme, encoded, ok := strings.Cut(req.Header.Get("Proxy-Authorization"), " ")
	if ok && strings.EqualFold(scheme, "Basic") {
		if decoded, err := base64.StdEncoding.DecodeString(encoded); err == nil {
			if user, _, ok := strings.Cut(string(decoded), ":"); ok {
				username = user
			}
		}
	}
	// Replay everything read from the socket, including bufio read-ahead.
	// Do not read or close req.Body: the original body belongs to the relay.
	return prefix.Bytes(), username, nil
}

func writeHTTPProxyError(conn net.Conn, status int) {
	if err := conn.SetWriteDeadline(time.Now().Add(clientReplyTimeout)); err != nil {
		return
	}
	fmt.Fprintf(conn, "HTTP/1.1 %d %s\r\nConnection: close\r\nContent-Length: 0\r\n\r\n", status, http.StatusText(status))
}

func (s *HTTPServer) handleConn(conn net.Conn) {
	defer s.releaseConn(conn)
	if err := conn.SetReadDeadline(time.Now().Add(httpHeaderTimeout)); err != nil {
		return
	}
	prefix, username, err := readHTTPPrefix(conn)
	if err != nil {
		if errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed) {
			return
		}
		status := http.StatusBadRequest
		var netErr net.Error
		if errors.Is(err, errHTTPHeaderTooLarge) {
			status = http.StatusRequestHeaderFieldsTooLarge
		} else if errors.As(err, &netErr) && netErr.Timeout() {
			status = http.StatusRequestTimeout
		}
		writeHTTPProxyError(conn, status)
		return
	}
	if err := conn.SetReadDeadline(time.Time{}); err != nil {
		return
	}
	backend, err := s.router.SelectBackendForProtocol(username, parseSession(username), "http")
	if err != nil {
		logx.Errorf("HTTP select backend failed: %v", err)
		writeHTTPProxyError(conn, http.StatusBadGateway)
		return
	}
	dialer := net.Dialer{Timeout: backendDialTimeout}
	backendConn, err := dialer.DialContext(s.ctx, "tcp", backend.Addr)
	if err != nil {
		logx.Errorf("HTTP dial backend %s failed: %v, source:%s", backend.Addr, err, conn.RemoteAddr())
		writeHTTPProxyError(conn, http.StatusBadGateway)
		return
	}
	if !s.trackConn(backendConn) {
		return
	}
	defer s.releaseConn(backendConn)
	throttled := s.limiter.WrapConn(backendConn)

	// The POP owns HTTP authentication, responses, and CONNECT negotiation.
	// Keep the connection pinned to the POP selected from the first request.
	relayTCP(conn, backendConn, io.MultiReader(bytes.NewReader(prefix), conn), throttled)
}
