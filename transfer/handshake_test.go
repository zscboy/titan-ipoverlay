package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"testing"
	"time"
)

func startHandshakeTestBackend(t *testing.T, protocol string, serve func(net.Conn) error) BackendInfo {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	accepted := make(chan net.Conn, 1)
	done := make(chan error, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			done <- err
			return
		}
		defer conn.Close()
		conn.SetDeadline(time.Now().Add(30 * time.Second))
		accepted <- conn
		done <- serve(conn)
	}()
	t.Cleanup(func() {
		listener.Close()
		select {
		case conn := <-accepted:
			conn.Close()
			if err := <-done; err != nil && !errors.Is(err, net.ErrClosed) {
				t.Errorf("test backend: %v", err)
			}
		case <-done:
		}
	})
	return BackendInfo{Type: protocol, Addr: listener.Addr().String()}
}

func TestBackendHandshakeTimeout(t *testing.T) {
	t.Parallel()
	for _, protocol := range []string{"http", "socks5"} {
		t.Run(protocol, func(t *testing.T) {
			t.Parallel()
			closed := make(chan struct{})
			backend := startHandshakeTestBackend(t, protocol, func(conn net.Conn) error {
				if protocol == "http" {
					if _, err := http.ReadRequest(bufio.NewReader(conn)); err != nil {
						return err
					}
					// Leave the headers incomplete to stall after the status line.
					if _, err := io.WriteString(conn, "HTTP/1.1 200 OK\r\nX-Test: incomplete"); err != nil {
						return err
					}
				}
				_, err := io.Copy(io.Discard, conn)
				close(closed)
				return err
			})
			start := time.Now()
			conn, err := ConnectBackend(backend, "127.0.0.1", 80, "", "")
			if conn != nil {
				conn.Close()
				t.Fatal("expected no connection after handshake timeout")
			}
			var netErr net.Error
			if !errors.As(err, &netErr) || !netErr.Timeout() {
				t.Fatalf("expected wrapped timeout error, got %v", err)
			}
			if elapsed := time.Since(start); elapsed > backendHandshakeTimeout+3*time.Second {
				t.Fatalf("handshake exceeded timeout budget: %v", elapsed)
			}
			select {
			case <-closed:
			case <-time.After(time.Second):
				t.Fatal("backend connection was not closed after timeout")
			}
		})
	}
}

func TestSocks5ClientHandshakeTimeout(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name string
		data []byte
	}{
		{name: "silent client"},
		{name: "partial greeting", data: []byte{5}},
		{name: "blocked method reply", data: []byte{5, 1, 0}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			serverConn, client := net.Pipe()
			defer serverConn.Close()
			defer client.Close()
			client.SetDeadline(time.Now().Add(clientHandshakeTimeout + 3*time.Second))
			done := make(chan struct{})
			go func() {
				NewSocks5Server("", nil, nil).handleConn(serverConn)
				close(done)
			}()
			if len(tt.data) > 0 {
				if _, err := client.Write(tt.data); err != nil {
					t.Fatal(err)
				}
			}
			select {
			case <-done:
			case <-time.After(clientHandshakeTimeout + 2*time.Second):
				t.Fatal("client handshake did not terminate on timeout")
			}
			if _, err := client.Read(make([]byte, 1)); err != io.EOF {
				t.Fatalf("expected closed connection, got %v", err)
			}
		})
	}
}

func acceptTestBackendHandshake(conn net.Conn, protocol string) error {
	if protocol == "http" {
		if _, err := http.ReadRequest(bufio.NewReader(conn)); err != nil {
			return err
		}
		_, err := io.WriteString(conn, "HTTP/1.1 200 Connection Established\r\n\r\n")
		return err
	}
	var greeting [3]byte
	if _, err := io.ReadFull(conn, greeting[:]); err != nil {
		return err
	}
	if greeting != [3]byte{5, 1, 0} {
		return fmt.Errorf("unexpected greeting: %v", greeting)
	}
	if _, err := conn.Write([]byte{5, 0}); err != nil {
		return err
	}
	var request [10]byte
	if _, err := io.ReadFull(conn, request[:]); err != nil {
		return err
	}
	_, err := conn.Write([]byte{5, 0, 0, 1, 0, 0, 0, 0, 0, 0})
	return err
}

func TestSocks5RelaySurvivesHandshakeDeadlines(t *testing.T) {
	t.Parallel()
	for _, protocol := range []string{"http", "socks5"} {
		t.Run(protocol, func(t *testing.T) {
			t.Parallel()
			backend := startHandshakeTestBackend(t, protocol, func(conn net.Conn) error {
				if err := acceptTestBackendHandshake(conn, protocol); err != nil {
					return err
				}
				_, err := io.Copy(conn, conn)
				return err
			})
			router := NewRouter([]BackendInfo{backend}, time.Hour, defaultRouterCleanupInterval)
			defer router.Close()
			serverConn, client := net.Pipe()
			defer serverConn.Close()
			defer client.Close()
			client.SetDeadline(time.Now().Add(20 * time.Second))
			done := make(chan struct{})
			go func() {
				NewSocks5Server("", router, nil).handleConn(serverConn)
				close(done)
			}()
			if err := handshakeSocks5(client, "", "", "127.0.0.1", 80); err != nil {
				t.Fatal(err)
			}
			// Both the client and backend handshake deadlines must be cleared.
			wait := max(backendHandshakeTimeout, clientHandshakeTimeout, clientReplyTimeout)
			<-time.After(wait + 100*time.Millisecond)
			if _, err := client.Write([]byte("ping")); err != nil {
				t.Fatalf("write after handshake deadline: %v", err)
			}
			var reply [4]byte
			if _, err := io.ReadFull(client, reply[:]); err != nil {
				t.Fatalf("read after handshake deadline: %v", err)
			}
			if string(reply[:]) != "ping" {
				t.Fatalf("unexpected relay data: %q", reply)
			}
			client.Close()
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("relay did not terminate after client close")
			}
		})
	}
}

func TestSocks5ClientReplyTimeout(t *testing.T) {
	t.Parallel()
	for _, success := range []bool{true, false} {
		t.Run(fmt.Sprintf("backend_success=%t", success), func(t *testing.T) {
			t.Parallel()
			backend := startHandshakeTestBackend(t, "http", func(conn net.Conn) error {
				if success {
					if err := acceptTestBackendHandshake(conn, "http"); err != nil {
						return err
					}
					_, err := io.Copy(io.Discard, conn)
					return err
				}
				if _, err := http.ReadRequest(bufio.NewReader(conn)); err != nil {
					return err
				}
				_, err := io.WriteString(conn, "HTTP/1.1 502 Bad Gateway\r\n\r\n")
				return err
			})
			router := NewRouter([]BackendInfo{backend}, time.Hour, defaultRouterCleanupInterval)
			defer router.Close()
			serverConn, client := net.Pipe()
			defer serverConn.Close()
			defer client.Close()
			client.SetDeadline(time.Now().Add(15 * time.Second))
			done := make(chan struct{})
			go func() {
				NewSocks5Server("", router, nil).handleConn(serverConn)
				close(done)
			}()
			if _, err := client.Write([]byte{5, 1, 0}); err != nil {
				t.Fatal(err)
			}
			var method [2]byte
			if _, err := io.ReadFull(client, method[:]); err != nil {
				t.Fatal(err)
			}
			if method != [2]byte{5, 0} {
				t.Fatalf("unexpected method response: %v", method)
			}
			if _, err := client.Write([]byte{5, 1, 0, 1, 127, 0, 0, 1, 0, 80}); err != nil {
				t.Fatal(err)
			}
			// Leave the final reply unread so that the server's Write blocks.
			select {
			case <-done:
			case <-time.After(clientReplyTimeout + 2*time.Second):
				t.Fatal("blocked final reply did not time out")
			}
			if _, err := client.Read(make([]byte, 1)); err != io.EOF {
				t.Fatalf("expected closed connection, got %v", err)
			}
		})
	}
}
