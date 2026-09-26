package main

import (
	"errors"
	"io"
	"net"

	"github.com/zeromicro/go-zero/core/logx"
)

type bufferedConn struct {
	net.Conn
	reader io.Reader
}

func (c *bufferedConn) Read(p []byte) (int, error) {
	return c.reader.Read(p)
}

func closeWrite(conn net.Conn) error {
	if writer, ok := conn.(interface{ CloseWrite() error }); ok {
		return writer.CloseWrite()
	}
	return conn.Close()
}

// Raw connections own lifecycle and half-closes; streams retain buffering and limits.
func relayTCP(client, backend net.Conn, clientReader io.Reader, backendStream net.Conn) {
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, err := io.Copy(backendStream, clientReader)
		if err == nil {
			err = closeWrite(backend)
		}
		if err != nil {
			if !errors.Is(err, net.ErrClosed) {
				logx.Errorf("Relay to POP %s failed: %v", backend.RemoteAddr(), err)
			}
			client.Close()
			backend.Close()
		}
	}()
	_, err := io.Copy(client, backendStream)
	if err == nil {
		err = closeWrite(client)
	}
	if err != nil {
		if !errors.Is(err, net.ErrClosed) {
			logx.Errorf("Relay from POP %s failed: %v", backend.RemoteAddr(), err)
		}
		client.Close()
		backend.Close()
	}
	<-done
}
