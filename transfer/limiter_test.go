package main

import (
	"bytes"
	"io"
	"net"
	"testing"
	"time"
)

func TestThrottledConn_RateLimit(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer serverConn.Close()
	defer clientConn.Close()

	// Rate limit read to 100 KB/s, burst 100 KB
	limiterMgr := NewLimiterManager(100*1024, 100*1024, 100*1024)

	throttledClient := limiterMgr.WrapConn(clientConn)

	data := bytes.Repeat([]byte("A"), 250*1024) // 250 KB

	start := time.Now()

	errChan := make(chan error, 1)
	go func() {
		_, err := serverConn.Write(data)
		errChan <- err
	}()

	buf := make([]byte, 32*1024)
	totalRead := 0
	for totalRead < len(data) {
		n, err := throttledClient.Read(buf)
		if err != nil {
			if err == io.EOF {
				break
			}
			t.Fatalf("Read error: %v", err)
		}
		totalRead += n
	}

	if err := <-errChan; err != nil {
		t.Fatalf("Write error: %v", err)
	}

	elapsed := time.Since(start)

	t.Logf("Transferred %d bytes in %v", totalRead, elapsed)

	if totalRead != len(data) {
		t.Errorf("Expected total read %d, got %d", len(data), totalRead)
	}

	// With 250KB total, burst 100KB, rate 100KB/s:
	// Initial 100KB is instant (burst), remaining 150KB should take approx 1.5 seconds.
	// Minimum expected time should be > 1.0 second.
	if elapsed < 1*time.Second {
		t.Errorf("Expected elapsed time >= 1s due to rate limit, got %v", elapsed)
	}
}
