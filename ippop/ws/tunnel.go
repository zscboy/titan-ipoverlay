package ws

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"
	"titan-ipoverlay/ippop/socks5"
	"titan-ipoverlay/ippop/ws/pb"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/zeromicro/go-zero/core/logx"
	"golang.org/x/time/rate"
	"google.golang.org/protobuf/proto"
)

const (
	netDelayCount   = 5
	limitRateBurst  = 128 * 1024
	failureCostTime = 600 // 600ms
	waitPongTimeout = 15
)

type TunOptions struct {
	Id    string
	OS    string
	VMAPI string
	IP    string
	// seconds
	UDPTimeout        int
	TCPTimeout        int
	DownloadRateLimti int64
	UploadRateLimit   int64
}

type TrafficStats struct {
	ReadDuration time.Duration
	RreadBytes   int64

	DataProcessStartTime time.Time
	DataProcessDuration  time.Duration
	// IdleDuration time.Duration

	WriteDuration time.Duration
	WriteBytes    int64

	StartTime time.Time
	EndTime   time.Time
}

// Tunnel Tunnel
type Tunnel struct {
	conn      *websocket.Conn
	writeLock sync.Mutex
	waitPong  int

	proxys sync.Map

	opts        *TunOptions
	waitList    sync.Map
	tunMgr      *TunnelManager
	waitLeaseCh chan bool
	// netDelays   []uint64
	//  bytes/sec
	readLimiter *rate.Limiter
	//  bytes/sec
	writeLimiter    *rate.Limiter
	rateLimiterLock sync.Mutex
	// if socks5 client connect with session
	userSessionID string
	delay         int64
	// This must be performed as a locking operation within addTunnel or swap tunnel
	index int

	trafficStats *TrafficStats

	IdleStartTime *time.Time
}

func newTunnel(conn *websocket.Conn, tunMgr *TunnelManager, opts *TunOptions) *Tunnel {
	t := &Tunnel{
		conn:            conn,
		opts:            opts,
		tunMgr:          tunMgr,
		rateLimiterLock: sync.Mutex{},
		trafficStats:    &TrafficStats{},
	}
	// logx.Debugf("opts:%#v", opts)
	t.setRateLimit(opts.DownloadRateLimti, opts.UploadRateLimit)

	conn.SetPingHandler(func(data string) error {
		t.waitPong = 0

		if err := t.writePong([]byte(data)); err != nil {
			logx.Errorf("writePong error:%s", err.Error())
		}
		return nil
	})

	conn.SetPongHandler(func(data string) error {
		t.onPong([]byte(data))
		return nil
	})

	return t
}

func (t *Tunnel) setRateLimit(downloadRateLimit, uploadRateLimit int64) {
	t.rateLimiterLock.Lock()
	defer t.rateLimiterLock.Unlock()

	if downloadRateLimit <= 0 {
		t.readLimiter = nil
	} else {
		if t.readLimiter == nil || t.readLimiter.Limit() != rate.Limit(downloadRateLimit) {
			t.readLimiter = rate.NewLimiter(rate.Limit(downloadRateLimit), limitRateBurst)
			logx.Debugf("tun %s new readLimiter", t.opts.Id)
		}
	}

	if uploadRateLimit <= 0 {
		t.writeLimiter = nil
	} else {
		if t.writeLimiter == nil || t.writeLimiter.Limit() != rate.Limit(uploadRateLimit) {
			t.writeLimiter = rate.NewLimiter(rate.Limit(uploadRateLimit), limitRateBurst)
			logx.Debugf("tun %s new writerLimiter for", t.opts.Id)
		}
	}
}

func (t *Tunnel) writePong(msg []byte) error {
	t.writeLock.Lock()
	defer t.writeLock.Unlock()

	if t.conn == nil {
		return fmt.Errorf("writePong, t.conn == nil, id:%s", t.opts.Id)
	}
	// t.conn.SetWriteDeadline(time.Now().Add(30 * time.Second))
	return t.conn.WriteMessage(websocket.PongMessage, msg)
}

func (t *Tunnel) writePing(msg []byte) error {
	t.writeLock.Lock()
	defer t.writeLock.Unlock()

	if t.conn == nil {
		return fmt.Errorf("writePong, t.conn == nil, id:%s", t.opts.Id)
	}

	// t.conn.SetWriteDeadline(time.Now().Add(30 * time.Second))
	return t.conn.WriteMessage(websocket.PingMessage, msg)
}

func (t *Tunnel) onPong(data []byte) {
	if len(data) != 8 {
		logx.Error("Invalid pong data")
		return
	}

	timestamp := int64(binary.LittleEndian.Uint64(data))
	t.delay = time.Since(time.UnixMicro(timestamp)).Milliseconds()

	t.waitPong = 0
}

func (t *Tunnel) serve() {
	for {
		_, message, err := t.readMessageWithLimitRate()
		if err != nil {
			if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway, websocket.CloseNoStatusReceived) {
				logx.Infof("Tunnel %s %s closed: %v", t.opts.Id, t.opts.IP, err)
			} else if errors.Is(err, net.ErrClosed) {
				logx.Infof("Tunnel %s %s connection closed", t.opts.Id, t.opts.IP)
			} else {
				logx.Errorf("Tunnel %s %s read failed: %v", t.opts.Id, t.opts.IP, err)
			}
			break
		}

		err = t.onMessage(message)
		if err != nil {
			logx.Errorf("Tunnel.serve onMessage failed: %v", err)
		}
	}

	t.onClose()
	t.conn = nil
	// logx.Infof("Tunnel %s %s close", t.opts.Id, t.opts.IP)
}

func (t *Tunnel) onMessage(data []byte) error {
	msg := &pb.Message{}
	err := proto.Unmarshal(data, msg)
	if err != nil {
		return err
	}

	switch msg.Type {
	case pb.MessageType_COMMAND:
		// return t.onControlMessage(msg.GetSessionId(), msg.Payload)
	case pb.MessageType_PROXY_SESSION_CREATE:
		return t.onProxySessionCreateReply(msg.GetSessionId(), msg.Payload)
	case pb.MessageType_PROXY_SESSION_DATA:
		return t.onProxySessionDataFromTunnel(msg.GetSessionId(), msg.Payload)
	case pb.MessageType_PROXY_SESSION_CLOSE:
		return t.onProxySessionClose(msg.GetSessionId())
	case pb.MessageType_PROXY_UDP_DATA:
		return t.onProxyUDPDataFromTunnel(msg.GetSessionId(), msg.Payload)
	default:
		logx.Errorf("Tunnel.onMessage, unsupport message type:%d", msg.Type)

	}
	return nil
}

func (t *Tunnel) onProxySessionCreateReply(sessionID string, payload []byte) error {
	// logx.Debugf("Tunnel.onProxySessionCreateComplete")
	v, ok := t.waitList.Load(sessionID)
	if !ok || v == nil {
		return fmt.Errorf("Tunnel.onProxySessionCreateComplete not found session:%s", sessionID)
	}

	ch := v.(chan []byte)

	select {
	case ch <- payload:
	default:
		logx.Errorf("Tunnel.onProxySessionCreateComplete: channel full or no listener for session %s", sessionID)
	}
	return nil
}

func (t *Tunnel) onProxySessionClose(sessionID string) error {
	// logx.Debugf("Tunnel.onProxySessionClose session id: %s", sessionID)
	v, ok := t.proxys.Load(sessionID)
	if !ok {
		logx.Debugf("Tunnel.onProxySessionClose, can not found session %s", sessionID)
		return nil
	}

	session := v.(*TCPProxy)
	session.closeByClient()

	return nil
}

func (t *Tunnel) onProxySessionDataFromTunnel(sessionID string, data []byte) error {
	// logx.Debugf("Tunnel.onProxySessionDataFromTunnel session id: %s", sessionID)
	v, ok := t.proxys.Load(sessionID)
	if !ok {
		t.onProxyTCPConnClose(sessionID)
		logx.Debugf("Tunnel.onProxySessionDataFromTunnel, can not found session %s", sessionID)
		return nil
	}

	proxy := v.(*TCPProxy)
	return proxy.write(data)
}

func (t *Tunnel) onProxyTCPConnClose(sessionID string) {
	logx.Debugf("Tunnel.onProxyTCPConnClose, session id: %s", sessionID)
	msg := &pb.Message{}
	msg.Type = pb.MessageType_PROXY_SESSION_CLOSE
	msg.SessionId = sessionID
	msg.Payload = nil

	buf, err := proto.Marshal(msg)
	if err != nil {
		logx.Errorf("Tunnel.onProxyConnClose, EncodeMessage failed:%s", err.Error())
		return
	}

	if err = t.write(buf); err != nil {
		logx.Errorf("Tunnel.onProxyConnClose, write message to tunnel failed:%s", err.Error())
	}
}

func (t *Tunnel) onProxyDataFromProxy(sessionID string, data []byte) {
	msg := &pb.Message{}
	msg.Type = pb.MessageType_PROXY_SESSION_DATA
	msg.SessionId = sessionID
	msg.Payload = data

	buf, err := proto.Marshal(msg)
	if err != nil {
		logx.Errorf("Tunnel.onProxyDataFromProxy proto message failed:%s", err.Error())
		return
	}

	if err = t.write(buf); err != nil {
		logx.Errorf("Tunnel.onProxyDataFromProxy, write message to tunnel failed:%s", err.Error())
	}

}

func (t *Tunnel) acceptSocks5TCPConn(conn net.Conn, targetInfo *socks5.SocksTargetInfo) error {
	logx.Debugf("acceptSocks5TCPConn, dest %s:%d", targetInfo.DomainName, targetInfo.Port)

	var healthStats *HealthStats
	v, ok := t.tunMgr.HealthStatsMap.Load(t.opts.Id)
	if ok {
		healthStats = v.(*HealthStats)
	}

	now := time.Now()

	sessionID := uuid.NewString()
	proxyTCP := newTCPProxy(sessionID, conn, t, targetInfo.Username)

	t.proxys.Store(sessionID, proxyTCP)
	defer t.proxys.Delete(sessionID)

	addr := fmt.Sprintf("%s:%d", targetInfo.DomainName, targetInfo.Port)
	err := t.onClientCreateByDomain(&pb.DestAddr{Addr: addr}, sessionID)
	if err != nil {
		healthStats.failureCount++
		return fmt.Errorf("Tunnel.acceptSocks5TCPConn client create by Domain failed, cost:%dms, addr:%s, err:%v, tun ip:%s, failure/success[%d/%d]", time.Since(now).Milliseconds(), addr, err, t.opts.IP, healthStats.failureCount, healthStats.successCount)
	}

	healthStats.successCount++

	// logx.Debugf("acceptSocks5TCPConn, create session cost:%dms, %s:%d total connect cost:%dms, tun ip:%s, failure/success[%d/%d]",
	// 	time.Since(now).Milliseconds(), targetInfo.DomainName, targetInfo.Port, time.Since(targetInfo.ConnCreateTime).Milliseconds(), t.opts.IP, healthStats.failureCount, healthStats.successCount)

	if len(targetInfo.ExtraBytes) > 0 {
		t.onProxyDataFromProxy(sessionID, targetInfo.ExtraBytes)
	}

	return proxyTCP.proxyConn()
}

func (t *Tunnel) onClientCreateByDomain(dest *pb.DestAddr, sessionID string) error {
	logx.Debugf("Tunnel.onClientCreateByDomain, dest %s", dest.Addr)

	buf, err := proto.Marshal(dest)
	if err != nil {
		return err
	}

	msg := &pb.Message{}
	msg.Type = pb.MessageType_PROXY_SESSION_CREATE
	msg.SessionId = sessionID
	msg.Payload = buf

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(t.opts.TCPTimeout)*time.Second)
	defer cancel()

	reply, err := t.requestCreateProxySession(ctx, msg)
	if err != nil {
		return err
	}

	if !reply.Success {
		return fmt.Errorf(reply.ErrMsg)
	}

	return nil

}

func (t *Tunnel) requestCreateProxySession(ctx context.Context, in *pb.Message) (*pb.CreateSessionReply, error) {
	data, err := proto.Marshal(in)
	if err != nil {
		return nil, err
	}

	ch := make(chan []byte)
	t.waitList.Store(in.GetSessionId(), ch)
	defer t.waitList.Delete(in.GetSessionId())

	err = t.write(data)
	if err != nil {
		return nil, err
	}

	for {
		select {
		case data := <-ch:
			out := &pb.CreateSessionReply{}
			err = proto.Unmarshal(data, out)
			if err != nil {
				return nil, fmt.Errorf("can not unmarshal replay:%s", err.Error())
			}
			return out, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

}

func (t *Tunnel) onProxyUDPDataFromTunnel(sessionID string, data []byte) error {
	proxy, ok := t.proxys.Load(sessionID)
	if !ok {
		logx.Debugf("Tunnel.onProxyUDPDataFromTunnel session %s not exist", sessionID)
		return nil
	}
	udp := proxy.(*UDPProxy)

	return udp.writeToSrc(data)
}

func (t *Tunnel) acceptSocks5UDPData(conn socks5.UDPConn, udpInfo *socks5.Socks5UDPInfo, data []byte) error {
	sessionID := uuid.NewSHA1(uuid.NameSpaceDNS, []byte(fmt.Sprintf("%s%s%s", udpInfo.UDPServerID, udpInfo.Src, udpInfo.Dest))).String()
	v, ok := t.proxys.Load(sessionID)
	if ok {
		return v.(*UDPProxy).writeToDest(data)
	}

	udp := newProxyUDP(sessionID, conn, udpInfo, t, t.opts.UDPTimeout)
	go udp.waitTimeout()

	logx.Debugf("Tunnel.acceptSocks5UDPData new UDPProxy %s", sessionID)

	t.proxys.Store(sessionID, udp)
	return udp.writeToDest(data)
}

func (t *Tunnel) keepalive() {
	if t.waitPong > waitPongTimeout {
		if t.conn != nil {
			t.conn.Close()
			t.conn = nil
			logx.Errorf("keepalive timeout, close tunnel %s connection", t.opts.Id)
		} else {
			logx.Errorf("keepalive timeout, tunnel %s already close", t.opts.Id)
		}

		return
	}

	b := make([]byte, 8)
	now := time.Now().UnixMicro()
	binary.LittleEndian.PutUint64(b, uint64(now))

	if err := t.writePing(b); err != nil {
		logx.Errorf("Tunnel.keepalive writePing failed:%v", err.Error())
	}

	if t.waitPong > 3 {
		logx.Debugf("tunnel %s keepalive send ping, waitPong:%d, delay:%d", t.opts.Id, t.waitPong, t.delay)
	}

	t.waitPong++
}

func (t *Tunnel) write(msg []byte) error {
	t.writeLock.Lock()
	defer t.writeLock.Unlock()

	return t.writeMessageWithLimitRate(websocket.BinaryMessage, msg)
}

func (t *Tunnel) readMessageWithLimitRate() (int, []byte, error) {
	if t.conn == nil {
		return 0, nil, fmt.Errorf("t.conn == nil")
	}

	startTime := time.Now()

	messageType, data, err := t.conn.ReadMessage()
	if err != nil {
		return 0, nil, err
	}

	// TODO: need to sub idle time
	t.trafficStats.ReadDuration += time.Now().Sub(startTime)
	t.trafficStats.RreadBytes += int64(len(data))
	t.trafficStats.DataProcessStartTime = time.Now()

	t.waitPong = 0

	readLimiter := t.readLimiter
	if readLimiter != nil {
		t.readLimiter.WaitN(context.TODO(), len(data))
	}

	return messageType, data, nil
}

func (t *Tunnel) writeMessageWithLimitRate(messageType int, data []byte) error {
	if t.conn == nil {
		return fmt.Errorf("tunnel.write t.conn == nil  id: %s", t.opts.Id)
	}

	writeLimiter := t.writeLimiter
	if writeLimiter != nil {
		writeLimiter.WaitN(context.TODO(), len(data))
	}

	return t.conn.WriteMessage(messageType, data)
}

func (t *Tunnel) waitClose() {
	if t.conn != nil {
		t.waitLeaseCh = make(chan bool)
		t.conn.Close()
		<-t.waitLeaseCh
		logx.Debugf("tunnel %s wait close", t.opts.Id)
	}
}

func (t *Tunnel) onClose() {
	t.clearProxys()
}

func (t *Tunnel) clearProxys() {
	t.proxys.Range(func(key, value any) bool {
		session, ok := value.(*TCPProxy)
		if ok {
			session.close()
		}
		return true
	})
	t.proxys.Clear()
}

func (t *Tunnel) leaseComplete() {
	if t.waitLeaseCh != nil {
		t.waitLeaseCh <- true
	}
}

func (t *Tunnel) getTrafficStats() *TrafficStats {
	return t.trafficStats
}

func (t *Tunnel) addTrafficStats(lenOfBytes int, duration time.Duration) {
	t.trafficStats.WriteBytes += int64(lenOfBytes)
	t.trafficStats.WriteDuration += duration
	t.trafficStats.DataProcessDuration = time.Now().Sub(t.trafficStats.DataProcessStartTime) - t.trafficStats.WriteDuration
}
