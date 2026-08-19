package main

import (
	"context"
	"net"

	"golang.org/x/time/rate"
)

type LimiterManager struct {
	readLimiter  *rate.Limiter
	writeLimiter *rate.Limiter
}

func NewLimiterManager(readBytesPerSec, writeBytesPerSec int64, burstBytes int) *LimiterManager {
	var readLimiter *rate.Limiter
	if readBytesPerSec > 0 {
		burst := burstBytes
		if burst <= 0 {
			burst = int(readBytesPerSec)
			if burst < 1024*1024 {
				burst = 1024 * 1024
			}
		}
		readLimiter = rate.NewLimiter(rate.Limit(readBytesPerSec), burst)
	}

	var writeLimiter *rate.Limiter
	if writeBytesPerSec > 0 {
		burst := burstBytes
		if burst <= 0 {
			burst = int(writeBytesPerSec)
			if burst < 1024*1024 {
				burst = 1024 * 1024
			}
		}
		writeLimiter = rate.NewLimiter(rate.Limit(writeBytesPerSec), burst)
	}

	return &LimiterManager{
		readLimiter:  readLimiter,
		writeLimiter: writeLimiter,
	}
}

func (m *LimiterManager) WrapConn(conn net.Conn) net.Conn {
	if m == nil || (m.readLimiter == nil && m.writeLimiter == nil) {
		return conn
	}
	return &ThrottledConn{
		Conn:         conn,
		readLimiter:  m.readLimiter,
		writeLimiter: m.writeLimiter,
	}
}

type ThrottledConn struct {
	net.Conn
	readLimiter  *rate.Limiter
	writeLimiter *rate.Limiter
}

func (c *ThrottledConn) Read(b []byte) (n int, err error) {
	n, err = c.Conn.Read(b)
	if n > 0 && c.readLimiter != nil {
		ctx := context.Background()
		burst := c.readLimiter.Burst()
		for offset := 0; offset < n; {
			chunkSize := n - offset
			if chunkSize > burst {
				chunkSize = burst
			}
			if waitErr := c.readLimiter.WaitN(ctx, chunkSize); waitErr != nil {
				return offset, waitErr
			}
			offset += chunkSize
		}
	}
	return n, err
}

func (c *ThrottledConn) Write(b []byte) (n int, err error) {
	if len(b) > 0 && c.writeLimiter != nil {
		ctx := context.Background()
		burst := c.writeLimiter.Burst()
		for offset := 0; offset < len(b); {
			chunkSize := len(b) - offset
			if chunkSize > burst {
				chunkSize = burst
			}
			if err := c.writeLimiter.WaitN(ctx, chunkSize); err != nil {
				return offset, err
			}
			wn, werr := c.Conn.Write(b[offset : offset+chunkSize])
			offset += wn
			if werr != nil {
				return offset, werr
			}
		}
		return len(b), nil
	}
	return c.Conn.Write(b)
}
