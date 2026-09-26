package main

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"net"
	"strings"
	"testing"
	"time"
)

func startSocks5Transfer(t *testing.T, backends []BackendInfo) *Socks5Server {
	t.Helper()
	router := NewRouter(backends, time.Hour, defaultRouterCleanupInterval)
	t.Cleanup(router.Close)
	server := NewSocks5Server("127.0.0.1:0", router, NewLimiterManager(1<<20, 1<<20, 1024))
	if err := server.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(server.Stop)
	return server
}

func dialSocks5Transfer(t *testing.T, server *Socks5Server) *net.TCPConn {
	t.Helper()
	conn, err := net.DialTimeout("tcp", server.listener.Addr().String(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	conn.SetDeadline(time.Now().Add(5 * time.Second))
	return conn.(*net.TCPConn)
}

func socksAuthBytes(username, password string) []byte {
	auth := append([]byte{1, byte(len(username))}, []byte(username)...)
	auth = append(auth, byte(len(password)))
	return append(auth, []byte(password)...)
}

func expectBytes(r io.Reader, want []byte) error {
	got := make([]byte, len(want))
	if _, err := io.ReadFull(r, got); err != nil {
		return err
	}
	if !bytes.Equal(got, want) {
		return fmt.Errorf("received %x, want %x", got, want)
	}
	return nil
}

func TestSocks5ServerRequiresSocks5Backend(t *testing.T) {
	router := NewRouter([]BackendInfo{{Type: "http", Addr: "localhost:8080"}}, time.Hour, defaultRouterCleanupInterval)
	defer router.Close()
	server := NewSocks5Server("127.0.0.1:0", router, nil)
	defer server.Stop()
	if err := server.Start(); err == nil || !strings.Contains(err.Error(), "socks5://") {
		t.Fatalf("expected missing SOCKS5 backend error, got %v", err)
	}
}

func TestSocks5PassthroughPreservesPackets(t *testing.T) {
	for _, target := range [][]byte{
		{1, 192, 0, 2, 1, 1, 187},
		append(append([]byte{3, 9}, []byte("a.example")...), 1, 187),
		{4, 0x20, 1, 0x0d, 0xb8, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1, 1, 187},
	} {
		t.Run(fmt.Sprintf("address_type_%d", target[0]), func(t *testing.T) {
			auth := socksAuthBytes("user-session-fixed", "dummy-password")
			request := append([]byte{5, 1, 0}, target...)
			reply := append(append([]byte{5, 0, 0, 3, 8}, []byte("pop.bind")...), 0x12, 0x34)
			payload := []byte("CLIENT-DATA")
			backend := startHandshakeTestBackend(t, "socks5", func(conn net.Conn) error {
				if err := expectBytes(conn, []byte{5, 1, 2}); err != nil {
					return err
				}
				if _, err := conn.Write([]byte{5, 2}); err != nil {
					return err
				}
				if err := expectBytes(conn, auth); err != nil {
					return err
				}
				if _, err := conn.Write([]byte{1, 0}); err != nil {
					return err
				}
				if err := expectBytes(conn, request); err != nil {
					return err
				}
				if _, err := conn.Write(append(append([]byte{}, reply...), []byte("EARLY")...)); err != nil {
					return err
				}
				got, err := io.ReadAll(conn)
				if err != nil || !bytes.Equal(got, payload) {
					return fmt.Errorf("client payload differs: %q, err=%v", got, err)
				}
				_, err = conn.Write([]byte("FINAL"))
				return err
			})
			server := startSocks5Transfer(t, []BackendInfo{{Type: "http", Addr: "localhost:1"}, backend})
			client := dialSocks5Transfer(t, server)
			if _, err := client.Write([]byte{5, 2, 0, 2}); err != nil {
				t.Fatal(err)
			}
			if err := expectBytes(client, []byte{5, 2}); err != nil {
				t.Fatal(err)
			}
			wire := append(append(append([]byte{}, auth...), request...), payload...)
			if _, err := client.Write(wire); err != nil {
				t.Fatal(err)
			}
			client.CloseWrite()
			got, err := io.ReadAll(client)
			want := append(append([]byte{1, 0}, reply...), []byte("EARLYFINAL")...)
			if err != nil || !bytes.Equal(got, want) {
				t.Fatalf("POP replies or payload changed: got %x, want %x, err=%v", got, want, err)
			}
		})
	}
}

func TestSocks5PassthroughAuthenticationFailure(t *testing.T) {
	for _, downgrade := range []bool{false, true} {
		t.Run(fmt.Sprintf("downgrade_%t", downgrade), func(t *testing.T) {
			auth := socksAuthBytes("user-session-fixed", "dummy")
			backend := startHandshakeTestBackend(t, "socks5", func(conn net.Conn) error {
				if err := expectBytes(conn, []byte{5, 1, 2}); err != nil {
					return err
				}
				if downgrade {
					conn.Write([]byte{5, 0})
				} else {
					conn.Write([]byte{5, 2})
					if err := expectBytes(conn, auth); err != nil {
						return err
					}
					conn.Write([]byte{1, 0x45})
				}
				rest, err := io.ReadAll(conn)
				if err != nil || len(rest) != 0 {
					return fmt.Errorf("forwarded data after rejected authentication: %x, err=%v", rest, err)
				}
				return nil
			})
			server := startSocks5Transfer(t, []BackendInfo{backend})
			client := dialSocks5Transfer(t, server)
			client.Write([]byte{5, 1, 2})
			if err := expectBytes(client, []byte{5, 2}); err != nil {
				t.Fatal(err)
			}
			client.Write(append(auth, []byte{5, 1, 0, 1, 127, 0, 0, 1, 0, 80}...))
			got, err := io.ReadAll(client)
			want := []byte{1, 0x45}
			if downgrade {
				want = []byte{1, 1}
			}
			if err != nil || !bytes.Equal(got, want) {
				t.Fatalf("unexpected authentication result: %x, err=%v", got, err)
			}
		})
	}
}

func TestSocks5PassthroughConnectFailure(t *testing.T) {
	reply := []byte{5, 5, 0, 1, 192, 0, 2, 20, 0x12, 0x34}
	backend := startHandshakeTestBackend(t, "socks5", func(conn net.Conn) error {
		if err := expectBytes(conn, []byte{5, 1, 0}); err != nil {
			return err
		}
		conn.Write([]byte{5, 0})
		if err := expectBytes(conn, []byte{5, 1, 0, 1, 127, 0, 0, 1, 0, 80}); err != nil {
			return err
		}
		_, err := conn.Write(reply)
		return err
	})
	server := startSocks5Transfer(t, []BackendInfo{backend})
	client := dialSocks5Transfer(t, server)
	client.Write([]byte{5, 1, 0})
	if err := expectBytes(client, []byte{5, 0}); err != nil {
		t.Fatal(err)
	}
	client.Write([]byte{5, 1, 0, 1, 127, 0, 0, 1, 0, 80})
	got, err := io.ReadAll(client)
	if err != nil || !bytes.Equal(got, reply) {
		t.Fatalf("POP failure reply changed: %x, err=%v", got, err)
	}
}

func TestSocks5PassthroughPOPTimeout(t *testing.T) {
	t.Parallel()
	for _, stage := range []string{"method", "authentication", "connect"} {
		t.Run(stage, func(t *testing.T) {
			t.Parallel()
			backend := startHandshakeTestBackend(t, "socks5", func(conn net.Conn) error {
				var greeting [3]byte
				if _, err := io.ReadFull(conn, greeting[:]); err != nil {
					return err
				}
				if stage != "method" {
					conn.Write([]byte{5, greeting[2]})
					if stage == "authentication" {
						if _, _, err := readSocks5Auth(conn); err != nil {
							return err
						}
					} else if _, err := readSocks5Packet(conn); err != nil {
						return err
					}
				}
				_, err := io.Copy(io.Discard, conn)
				return err
			})
			server := startSocks5Transfer(t, []BackendInfo{backend})
			client := dialSocks5Transfer(t, server)
			client.SetDeadline(time.Now().Add(backendHandshakeTimeout + 5*time.Second))
			username := ""
			if stage == "authentication" {
				username = "user-session-fixed"
			}
			start := time.Now()
			if err := handshakeSocks5(client, username, "dummy", "127.0.0.1", 80); err == nil {
				t.Fatal("stalled POP handshake unexpectedly succeeded")
			}
			if elapsed := time.Since(start); elapsed > backendHandshakeTimeout+3*time.Second {
				t.Fatalf("POP handshake exceeded its timeout budget: %v", elapsed)
			}
		})
	}
}

func TestSocks5ServerStopClosesConnections(t *testing.T) {
	ready := make(chan struct{})
	closed := make(chan struct{})
	backend := startHandshakeTestBackend(t, "socks5", func(conn net.Conn) error {
		if err := acceptTestBackendHandshake(conn, "socks5"); err != nil {
			return err
		}
		close(ready)
		_, err := io.Copy(io.Discard, conn)
		close(closed)
		return err
	})
	server := startSocks5Transfer(t, []BackendInfo{backend})
	client := dialSocks5Transfer(t, server)
	if err := handshakeSocks5(client, "", "", "127.0.0.1", 80); err != nil {
		t.Fatal(err)
	}
	<-ready
	server.Stop()
	if _, err := client.Read(make([]byte, 1)); err == nil {
		t.Fatal("client remained connected after Stop")
	}
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("POP remained connected after Stop")
	}
}

func startSessionTestPOP(t *testing.T, id byte) BackendInfo {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			func() {
				defer conn.Close()
				conn.SetDeadline(time.Now().Add(3 * time.Second))
				reader := bufio.NewReader(conn)
				var greeting [3]byte
				if _, err := io.ReadFull(reader, greeting[:]); err != nil {
					t.Error(err)
					return
				}
				conn.Write([]byte{5, greeting[2]})
				if greeting[2] == 2 {
					if _, _, err := readSocks5Auth(reader); err != nil {
						t.Error(err)
						return
					}
					conn.Write([]byte{1, 0})
				}
				if _, err := readSocks5Packet(reader); err != nil {
					t.Error(err)
					return
				}
				conn.Write([]byte{5, 0, 0, 1, 127, 0, 0, 1, 0, id})
			}()
		}
	}()
	t.Cleanup(func() { listener.Close(); <-done })
	return BackendInfo{Type: "socks5", Addr: listener.Addr().String()}
}

func TestSocks5PassthroughSessionRouting(t *testing.T) {
	a, b := startSessionTestPOP(t, 1), startSessionTestPOP(t, 2)
	server := startSocks5Transfer(t, []BackendInfo{{Type: "http", Addr: "localhost:1"}, a, b})
	for _, tt := range []struct {
		username string
		want     byte
	}{
		{username: "user-session-fixed", want: 1},
		{username: "user-session-fixed", want: 1},
		{username: "user-session-other", want: 2},
		{username: "", want: 1},
		{username: "", want: 2},
	} {
		client := dialSocks5Transfer(t, server)
		method := byte(0)
		if tt.username != "" {
			method = 2
		}
		client.Write([]byte{5, 1, method})
		if err := expectBytes(client, []byte{5, method}); err != nil {
			t.Fatal(err)
		}
		if method == 2 {
			client.Write(socksAuthBytes(tt.username, "dummy"))
			if err := expectBytes(client, []byte{1, 0}); err != nil {
				t.Fatal(err)
			}
		}
		client.Write([]byte{5, 1, 0, 1, 127, 0, 0, 1, 0, 80})
		if err := expectBytes(client, []byte{5, 0, 0, 1, 127, 0, 0, 1, 0, tt.want}); err != nil {
			t.Fatalf("session %q: %v", tt.username, err)
		}
		client.Close()
	}
}
