package main

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestHTTPServerStartReturnsListenError(t *testing.T) {
	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer occupied.Close()
	router := NewRouter([]BackendInfo{{Type: "http", Addr: "127.0.0.1:8080"}}, time.Hour, defaultRouterCleanupInterval)
	defer router.Close()

	for _, tt := range []struct {
		name string
		addr string
	}{
		{name: "occupied port", addr: occupied.Addr().String()},
		{name: "invalid port", addr: "127.0.0.1:80801"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			server := NewHTTPServer(tt.addr, router, nil)
			defer server.Stop()
			var netErr net.Error
			if err := server.Start(); !errors.As(err, &netErr) {
				t.Fatalf("Start should return a network error for an unusable listen address, got %v", err)
			}
		})
	}
}

func startHTTPTransfer(t *testing.T, backends []BackendInfo, limiter *LimiterManager) *HTTPServer {
	t.Helper()
	router := NewRouter(backends, time.Hour, defaultRouterCleanupInterval)
	t.Cleanup(router.Close)
	server := NewHTTPServer("127.0.0.1:0", router, limiter)
	if err := server.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(server.Stop)
	return server
}

func dialHTTPTransfer(t *testing.T, server *HTTPServer) *net.TCPConn {
	t.Helper()
	conn, err := net.DialTimeout("tcp", server.listener.Addr().String(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	conn.SetDeadline(time.Now().Add(5 * time.Second))
	return conn.(*net.TCPConn)
}

func TestHTTPServerRequiresHTTPBackend(t *testing.T) {
	router := NewRouter([]BackendInfo{{Type: "socks5", Addr: "127.0.0.1:1080"}}, time.Hour, defaultRouterCleanupInterval)
	defer router.Close()
	server := NewHTTPServer("127.0.0.1:0", router, nil)
	defer server.Stop()
	if err := server.Start(); err == nil || !strings.Contains(err.Error(), "http://") {
		t.Fatalf("expected missing HTTP backend error, got %v", err)
	}
}

func TestReadHTTPPrefixPreservesBufferedBytes(t *testing.T) {
	header := "POST http://a.example/upload HTTP/1.1\r\nHost: a.example\r\nProxy-Authorization: Basic " +
		base64.StdEncoding.EncodeToString([]byte("user-session-one:dummy")) + "\r\nContent-Length: 4\r\n\r\n"
	wire := header + "BODYGET http://b.example/ HTTP/1.1\r\nHost: b.example\r\n\r\n"
	input := strings.NewReader(wire)
	prefix, username, err := readHTTPPrefix(input)
	if err != nil {
		t.Fatal(err)
	}
	if username != "user-session-one" {
		t.Fatalf("unexpected username: %q", username)
	}
	if len(prefix) <= len(header) {
		t.Fatal("test did not exercise read-ahead past the header")
	}
	got, err := io.ReadAll(io.MultiReader(bytes.NewReader(prefix), input))
	if err != nil || string(got) != wire {
		t.Fatalf("replayed bytes differ: %q, err=%v", got, err)
	}
}

func TestReadHTTPPrefixRejectsOversizedHeader(t *testing.T) {
	wire := "GET http://a.example/ HTTP/1.1\r\nX-Large: " + strings.Repeat("x", http.DefaultMaxHeaderBytes)
	if _, _, err := readHTTPPrefix(strings.NewReader(wire)); !errors.Is(err, errHTTPHeaderTooLarge) {
		t.Fatalf("expected header limit error, got %v", err)
	}
}

func TestHTTPPassthroughPreservesWireBytes(t *testing.T) {
	for _, tt := range []struct {
		name     string
		request  string
		response string
	}{
		{
			name: "pipelined requests and body",
			request: "POST http://a.example/path?q=1 HTTP/1.1\r\nHost: a.example\r\n" +
				"pRoXy-AuThOrIzAtIoN: Basic dXNlcjpwYXNz\r\nX-Repeat: one\r\nX-Repeat: two\r\nContent-Length: 4\r\n\r\nBODY" +
				"GET http://b.example/ HTTP/1.1\r\nHost: b.example\r\nProxy-Authorization: Basic dXNlcjpwYXNz\r\n\r\n",
			response: "HTTP/1.1 200 OK\r\nContent-Length: 1\r\n\r\nAHTTP/1.1 200 OK\r\nContent-Length: 1\r\n\r\nB",
		},
		{
			name: "CONNECT and early tunnel bytes",
			request: "CONNECT a.example:443 HTTP/1.1\r\nHost: a.example:443\r\n" +
				"Proxy-Authorization: Basic dXNlcjpwYXNz\r\n\r\n\x16\x03\x01\x00\x05HELLO",
			response: "HTTP/1.1 200 Connection Established\r\nX-POP: original\r\n\r\n\x16\x03\x03SERVER",
		},
		{
			name:     "POP authentication failure",
			request:  "CONNECT a.example:443 HTTP/1.1\r\nHost: a.example:443\r\n\r\n",
			response: "HTTP/1.1 407 Proxy Authentication Required\r\nProxy-Authenticate: Basic realm=\"pop\"\r\nContent-Length: 6\r\nConnection: close\r\n\r\nDENIED",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			backend := startHandshakeTestBackend(t, "http", func(conn net.Conn) error {
				got, err := io.ReadAll(conn)
				if err != nil {
					return err
				}
				if string(got) != tt.request {
					return fmt.Errorf("POP received modified request: %q", got)
				}
				_, err = io.WriteString(conn, tt.response)
				return err
			})
			server := startHTTPTransfer(t, []BackendInfo{backend}, NewLimiterManager(1<<20, 1<<20, 1024))
			client := dialHTTPTransfer(t, server)
			if _, err := io.WriteString(client, tt.request); err != nil {
				t.Fatal(err)
			}
			if err := client.CloseWrite(); err != nil {
				t.Fatal(err)
			}
			got, err := io.ReadAll(client)
			if err != nil || string(got) != tt.response {
				t.Fatalf("POP response was modified or lost after client half-close: %q, err=%v", got, err)
			}
		})
	}
}

func TestHTTPPassthroughStreamsExpectContinueUpload(t *testing.T) {
	body := bytes.Repeat([]byte("x"), 2*http.DefaultMaxHeaderBytes)
	header := fmt.Sprintf("POST http://a.example/upload HTTP/1.1\r\nHost: a.example\r\nExpect: 100-continue\r\nContent-Length: %d\r\n\r\n", len(body))
	backend := startHandshakeTestBackend(t, "http", func(conn net.Conn) error {
		reader := bufio.NewReader(conn)
		req, err := http.ReadRequest(reader)
		if err != nil {
			return err
		}
		if req.Method != "POST" || req.RequestURI != "http://a.example/upload" {
			return fmt.Errorf("expected original POST, got %s %s", req.Method, req.RequestURI)
		}
		if _, err := io.WriteString(conn, "HTTP/1.1 100 Continue\r\n\r\n"); err != nil {
			return err
		}
		got, err := io.ReadAll(req.Body)
		if err != nil || !bytes.Equal(got, body) {
			return fmt.Errorf("upload differs: got %d bytes, err=%v", len(got), err)
		}
		_, err = io.WriteString(conn, "HTTP/1.1 201 Created\r\nContent-Length: 0\r\nConnection: close\r\n\r\n")
		return err
	})
	server := startHTTPTransfer(t, []BackendInfo{backend}, nil)
	client := dialHTTPTransfer(t, server)
	if _, err := io.WriteString(client, header); err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(client)
	resp, err := http.ReadResponse(reader, nil)
	if err != nil || resp.StatusCode != http.StatusContinue {
		t.Fatalf("expected POP's 100 Continue before sending body, got %v, err=%v", resp, err)
	}
	resp.Body.Close()
	if _, err := client.Write(body); err != nil {
		t.Fatal(err)
	}
	resp, err = http.ReadResponse(reader, nil)
	if err != nil || resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected POP's upload response, got %v, err=%v", resp, err)
	}
	resp.Body.Close()
}

func TestHTTPPassthroughSessionRouting(t *testing.T) {
	makePOP := func(id string) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-POP", id)
			w.Header().Set("X-Target", r.URL.Host)
			w.Header().Set("X-Received-Auth", r.Header.Get("Proxy-Authorization"))
			w.Write([]byte("OK"))
		}))
	}
	a, b := makePOP("A"), makePOP("B")
	defer a.Close()
	defer b.Close()
	server := startHTTPTransfer(t, []BackendInfo{
		{Type: "socks5", Addr: "127.0.0.1:1"},
		{Type: "http", Addr: a.Listener.Addr().String()},
		{Type: "http", Addr: b.Listener.Addr().String()},
	}, nil)
	proxyURL := &url.URL{Scheme: "http", Host: server.listener.Addr().String(), User: url.UserPassword("user-session-fixed", "dummy")}
	transport := &http.Transport{Proxy: http.ProxyURL(proxyURL)}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 3 * time.Second}
	for i, host := range []string{"a.example", "b.example", "c.example"} {
		if i == 2 {
			transport.CloseIdleConnections()
		}
		resp, err := client.Get("http://" + host + "/")
		if err != nil {
			t.Fatal(err)
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		if resp.Header.Get("X-POP") != "A" || resp.Header.Get("X-Target") != host || resp.Header.Get("X-Received-Auth") == "" {
			t.Fatalf("request %d routed or rewritten incorrectly: %v", i, resp.Header)
		}
	}
	for i, want := range []string{"B", "A"} {
		conn := dialHTTPTransfer(t, server)
		io.WriteString(conn, "GET http://a.example/ HTTP/1.1\r\nHost: a.example\r\nConnection: close\r\n\r\n")
		resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
		if err != nil {
			t.Fatal(err)
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		conn.Close()
		if resp.Header.Get("X-POP") != want {
			t.Fatalf("unbound connection %d: got POP %q, want %q", i, resp.Header.Get("X-POP"), want)
		}
	}
}

func TestHTTPServerStopClosesRelay(t *testing.T) {
	ready := make(chan struct{})
	closed := make(chan struct{})
	backend := startHandshakeTestBackend(t, "http", func(conn net.Conn) error {
		reader := bufio.NewReader(conn)
		if _, err := http.ReadRequest(reader); err != nil {
			return err
		}
		close(ready)
		_, err := io.Copy(io.Discard, reader)
		close(closed)
		return err
	})
	server := startHTTPTransfer(t, []BackendInfo{backend}, nil)
	client := dialHTTPTransfer(t, server)
	io.WriteString(client, "CONNECT a.example:443 HTTP/1.1\r\nHost: a.example:443\r\n\r\n")
	select {
	case <-ready:
	case <-time.After(time.Second):
		t.Fatal("POP did not receive the request")
	}
	server.Stop()
	if _, err := client.Read(make([]byte, 1)); err == nil {
		t.Fatal("client connection remained open after Stop")
	}
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("POP connection remained open after Stop")
	}
}

func TestHTTPPassthroughHeaderTimeout(t *testing.T) {
	t.Parallel()
	server := startHTTPTransfer(t, []BackendInfo{{Type: "http", Addr: "127.0.0.1:1"}}, nil)
	client := dialHTTPTransfer(t, server)
	client.SetDeadline(time.Now().Add(httpHeaderTimeout + 3*time.Second))
	io.WriteString(client, "GET http://a.example/ HTTP/1.1\r\nHost: a.example\r\n")
	resp, err := http.ReadResponse(bufio.NewReader(client), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusRequestTimeout {
		t.Fatalf("expected 408 for incomplete headers, got %s", resp.Status)
	}
}

func TestHTTPPassthroughClearsHeaderDeadline(t *testing.T) {
	t.Parallel()
	backend := startHandshakeTestBackend(t, "http", func(conn net.Conn) error {
		reader := bufio.NewReader(conn)
		if _, err := http.ReadRequest(reader); err != nil {
			return err
		}
		if _, err := io.WriteString(conn, "HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
			return err
		}
		_, err := io.Copy(conn, reader)
		return err
	})
	server := startHTTPTransfer(t, []BackendInfo{backend}, nil)
	client := dialHTTPTransfer(t, server)
	client.SetDeadline(time.Now().Add(httpHeaderTimeout + 5*time.Second))
	io.WriteString(client, "CONNECT a.example:443 HTTP/1.1\r\nHost: a.example:443\r\n\r\n")
	reader := bufio.NewReader(client)
	resp, err := http.ReadResponse(reader, &http.Request{Method: http.MethodConnect})
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("CONNECT failed: %v, err=%v", resp, err)
	}
	<-time.After(httpHeaderTimeout + 100*time.Millisecond)
	if _, err := io.WriteString(client, "ping"); err != nil {
		t.Fatal(err)
	}
	var reply [4]byte
	if _, err := io.ReadFull(reader, reply[:]); err != nil || string(reply[:]) != "ping" {
		t.Fatalf("tunnel stopped working after header deadline: %q, err=%v", reply, err)
	}
}
