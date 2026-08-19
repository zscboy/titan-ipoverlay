package main

import (
	"bytes"
	"encoding/base64"
	"errors"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"

	"github.com/zeromicro/go-zero/core/logx"
)

type HTTPServer struct {
	listenAddr string
	router     *Router
	limiter    *LimiterManager
	server     *http.Server
}

func NewHTTPServer(listenAddr string, router *Router, limiter *LimiterManager) *HTTPServer {
	return &HTTPServer{
		listenAddr: listenAddr,
		router:     router,
		limiter:    limiter,
	}
}

func (s *HTTPServer) Start() error {
	s.server = &http.Server{
		Addr:    s.listenAddr,
		Handler: s,
	}

	logx.Infof("HTTP Proxy Server listening on %s", s.listenAddr)
	go func() {
		if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logx.Errorf("HTTP Server error: %v", err)
		}
	}()
	return nil
}

func (s *HTTPServer) Stop() {
	if s.server != nil {
		s.server.Close()
	}
}

func (s *HTTPServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	session := ""
	clientUsername := ""
	clientPassword := ""
	auth := r.Header.Get("Proxy-Authorization")
	if strings.HasPrefix(auth, "Basic ") {
		payload, err := base64.StdEncoding.DecodeString(auth[len("Basic "):])
		if err == nil {
			pair := strings.SplitN(string(payload), ":", 2)
			if len(pair) == 2 {
				clientUsername = pair[0]
				clientPassword = pair[1]
				session = parseSession(clientUsername)
			}
		}
	}

	if r.Method == http.MethodConnect {
		s.handleConnect(w, r, session, clientUsername, clientPassword)
	} else {
		s.handleHTTP(w, r, session, clientUsername, clientPassword)
	}
}

func (s *HTTPServer) parseHostPort(hostPort string, defaultPort int) (string, int, error) {
	host, portStr, err := net.SplitHostPort(hostPort)
	if err != nil {
		if strings.Contains(err.Error(), "missing port") {
			return hostPort, defaultPort, nil
		}
		return hostPort, 0, err
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return host, 0, err
	}
	return host, port, nil
}

func (s *HTTPServer) handleConnect(w http.ResponseWriter, r *http.Request, session, clientUsername, clientPassword string) {
	host, port, err := s.parseHostPort(r.Host, 443)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	hij, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "Hijacking not supported", http.StatusInternalServerError)
		return
	}

	conn, _, err := hij.Hijack()
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	defer conn.Close()

	// Select Backend
	backend, err := s.router.SelectBackend(clientUsername, session)
	if err != nil {
		logx.Errorf("HTTP CONNECT select backend failed: %v", err)
		conn.Write([]byte("HTTP/1.1 502 Bad Gateway\r\n\r\n"))
		return
	}

	// Connect to Backend
	backendConn, err := ConnectBackend(backend, host, port, clientUsername, clientPassword)
	if err != nil {
		logx.Errorf("HTTP CONNECT to backend %s://%s failed: %v, Username:%s, Password:%s, source:%s, target:%s", backend.Type, backend.Addr, err, clientUsername, clientPassword, r.RemoteAddr, r.Host)
		conn.Write([]byte("HTTP/1.1 504 Gateway Timeout\r\n\r\n"))
		return
	}
	backendConn = s.limiter.WrapConn(backendConn)
	defer backendConn.Close()

	// Write success to client
	_, _ = conn.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n"))

	// Bidirectional relay
	go func() {
		_, relayErr := io.Copy(backendConn, conn)
		if relayErr != nil && !errors.Is(relayErr, net.ErrClosed) {
			logx.Errorf("[Downstream Connection Drop] target %s: %v", r.Host, relayErr)
		}
		backendConn.Close()
		conn.Close()
	}()
	_, relayErr := io.Copy(conn, backendConn)
	if relayErr != nil && !errors.Is(relayErr, net.ErrClosed) {
		logx.Errorf("[1007 Upstream Connection Drop] target %s: %v", r.Host, relayErr)
	}
	conn.Close()
	backendConn.Close()
}

func (s *HTTPServer) handleHTTP(w http.ResponseWriter, r *http.Request, session, clientUsername, clientPassword string) {
	host, port, err := s.parseHostPort(r.Host, 80)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	hij, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "Hijacking not supported", http.StatusInternalServerError)
		return
	}

	conn, _, err := hij.Hijack()
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	defer conn.Close()

	// Reconstruct the HTTP request
	r.Header.Del("Proxy-Authorization")
	r.RequestURI = "" // Force RequestURI empty so Write uses URL
	r.URL.Scheme = "" // Clear scheme to ensure it is written in non-proxy form
	r.URL.Host = ""   // Clear host to ensure it is written in non-proxy form

	var buf bytes.Buffer
	if err := r.Write(&buf); err != nil {
		logx.Errorf("HTTP request serialization failed: %v", err)
		conn.Write([]byte("HTTP/1.1 500 Internal Server Error\r\n\r\n"))
		return
	}

	// Select Backend
	backend, err := s.router.SelectBackend(clientUsername, session)
	if err != nil {
		logx.Errorf("HTTP select backend failed: %v", err)
		conn.Write([]byte("HTTP/1.1 502 Bad Gateway\r\n\r\n"))
		return
	}

	// Connect to Backend
	backendConn, err := ConnectBackend(backend, host, port, clientUsername, clientPassword)
	if err != nil {
		logx.Errorf("HTTP to backend %s://%s failed: %v", backend.Type, backend.Addr, err)
		conn.Write([]byte("HTTP/1.1 504 Gateway Timeout\r\n\r\n"))
		return
	}
	backendConn = s.limiter.WrapConn(backendConn)
	defer backendConn.Close()

	// Write serialized request to backend
	if _, err := backendConn.Write(buf.Bytes()); err != nil {
		logx.Errorf("HTTP write request to backend failed: %v", err)
		return
	}

	// Bidirectional relay
	go func() {
		_, relayErr := io.Copy(backendConn, conn)
		if relayErr != nil && !errors.Is(relayErr, net.ErrClosed) {
			logx.Errorf("[Downstream Connection Drop] target %s: %v", r.Host, relayErr)
		}
		backendConn.Close()
		conn.Close()
	}()
	_, relayErr := io.Copy(conn, backendConn)
	if relayErr != nil && !errors.Is(relayErr, net.ErrClosed) {
		logx.Errorf("[1007 Upstream Connection Drop] target %s: %v", r.Host, relayErr)
	}
	conn.Close()
	backendConn.Close()
}
