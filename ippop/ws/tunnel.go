package ws

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"
	"titan-ipoverlay/ippop/socks5"
	"titan-ipoverlay/ippop/ws/pb"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/zeromicro/go-zero/core/logx"
	"golang.org/x/mod/semver"
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
	Id          string
	OS          string
	VMAPI       string
	IP          string
	Version     string
	CountryCode string // 国家码，如 "US"
	// seconds
	UDPTimeout        int
	TCPTimeout        int
	DownloadRateLimti int64
	UploadRateLimit   int64
	IsBlacklisted     bool
}

type TrafficStats struct {
	ReadStartTime atomic.Pointer[time.Time]
	ReadDuration  atomic.Int64
	ReadBytes     atomic.Int64

	DataProcessStartTime atomic.Pointer[time.Time]
	DataProcessDuration  atomic.Int64

	WriteDuration atomic.Int64
	WriteBytes    atomic.Int64
}

// Tunnel Tunnel
type Tunnel struct {
	conn      *websocket.Conn
	writeLock sync.Mutex
	waitPong  atomic.Int32
	closed    atomic.Bool

	proxys ProxyMap

	opts     *TunOptions
	waitList sync.Map
	tunMgr   *TunnelManager
	ctx      context.Context
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

func newTunnel(conn *websocket.Conn, tunMgr *TunnelManager, opts *TunOptions, ctx context.Context) *Tunnel {
	t := &Tunnel{
		conn:            conn,
		opts:            opts,
		tunMgr:          tunMgr,
		rateLimiterLock: sync.Mutex{},
		trafficStats:    &TrafficStats{},
		ctx:             ctx,
	}
	t.setRateLimit(opts.DownloadRateLimti, opts.UploadRateLimit)

	conn.SetPingHandler(func(data string) error {
		t.waitPong.Store(0)

		if err := t.writePong([]byte(data)); err != nil {
			logx.Errorf("Tunnel %s %s writePong error:%s", t.opts.Id, t.opts.IP, err.Error())
		}
		return nil
	})

	conn.SetPongHandler(func(data string) error {
		t.onPong([]byte(data))
		return nil
	})

	return t
}

func (t *Tunnel) version() string {
	return t.opts.Version
}

func (t *Tunnel) setRateLimit(downloadRateLimit, uploadRateLimit int64) {
	t.rateLimiterLock.Lock()
	defer t.rateLimiterLock.Unlock()

	if downloadRateLimit <= 0 {
		t.readLimiter = nil
	} else {
		if t.readLimiter == nil || t.readLimiter.Limit() != rate.Limit(downloadRateLimit) {
			t.readLimiter = rate.NewLimiter(rate.Limit(downloadRateLimit), limitRateBurst)
		}
	}

	if uploadRateLimit <= 0 {
		t.writeLimiter = nil
	} else {
		if t.writeLimiter == nil || t.writeLimiter.Limit() != rate.Limit(uploadRateLimit) {
			t.writeLimiter = rate.NewLimiter(rate.Limit(uploadRateLimit), limitRateBurst)
			logx.Debugf("tun %s new writeLimiter", t.opts.Id)
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
		return fmt.Errorf("writePing, t.conn == nil, id:%s", t.opts.Id)
	}

	// t.conn.SetWriteDeadline(time.Now().Add(30 * time.Second))
	return t.conn.WriteMessage(websocket.PingMessage, msg)
}

func (t *Tunnel) onPong(data []byte) {
	if len(data) != 8 {
		logx.Errorf("Tunnel %s %s Invalid pong data", t.opts.Id, t.opts.IP)
		return
	}

	timestamp := int64(binary.LittleEndian.Uint64(data))
	t.delay = time.Since(time.UnixMicro(timestamp)).Milliseconds()
	// metrics.TunnelDelay.WithLabelValues(t.opts.Id, t.tunMgr.config.GetNodeID()).Set(float64(t.delay))

	t.waitPong.Store(0)
}

func (t *Tunnel) serve() {
	for {
		// T1 开始：由 readMessageWithLimitRate 内部精确捕获消息到达时间
		_, message, t1StartTime, t1EndTime, err := t.readMessageWithLimitRate()
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

		rawMessageSize := int64(len(message))

		// T2 开始：protobuf 解码和消息处理
		err = t.onMessage(message, t1StartTime, t1EndTime, rawMessageSize)
		if err != nil {
			logx.Errorf("Tunnel %s %s onMessage failed: %v", t.opts.Id, t.opts.IP, err)
		}
	}

	t.close()
}

func (t *Tunnel) onMessage(data []byte, t1StartTime, t1EndTime time.Time, rawMessageSize int64) error {
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
		return t.onProxySessionDataFromTunnel(msg.GetSessionId(), msg.Payload, t1StartTime, t1EndTime, rawMessageSize)
	case pb.MessageType_PROXY_SESSION_CLOSE:
		return t.onProxySessionClose(msg.GetSessionId())
	case pb.MessageType_PROXY_SESSION_HALF_CLOSE:
		return t.onProxySessionHalfClose(msg.GetSessionId())
	case pb.MessageType_PROXY_UDP_DATA:
		return t.onProxyUDPDataFromTunnel(msg.GetSessionId(), msg.Payload)
	default:
		logx.Errorf("Tunnel %s %s onMessage, unsupported message type:%d", t.opts.Id, t.opts.IP, msg.Type)

	}
	return nil
}

func (t *Tunnel) notifySessionCreateRequest(sessionID string, payload []byte) error {
	v, ok := t.waitList.Load(sessionID)
	if !ok || v == nil {
		return fmt.Errorf("Tunnel.onProxySessionCreateReply not found session:%s", sessionID)
	}

	ch := v.(chan []byte)
	select {
	case ch <- payload:
	default:
		logx.Errorf("Tunnel %s %s onProxySessionCreateReply: channel full or no listener for session %s", t.opts.Id, t.opts.IP, sessionID)
	}
	return nil
}

func (t *Tunnel) onProxySessionCreateReply(sessionID string, payload []byte) error {
	if !t.isNodeVersionGreatThanV011() {
		return t.notifySessionCreateRequest(sessionID, payload)
	}

	v, ok := t.proxys.Load(sessionID)
	if !ok || v == nil {
		return fmt.Errorf("Tunnel.onProxySessionCreateReply not found session:%s", sessionID)
	}

	reply := &pb.CreateSessionReply{}
	err := proto.Unmarshal(payload, reply)
	if err != nil {
		return fmt.Errorf("can not unmarshal CreateSessionReply:%s", err.Error())
	}

	if reply.Success {
		logx.Debugf("Tunnel %s %s onProxySessionCreateReply, create session %s success", t.opts.Id, t.opts.IP, sessionID)
	} else {
		logx.Errorf("Tunnel %s %s onProxySessionCreateReply, create session %s failed: %s", t.opts.Id, t.opts.IP, sessionID, reply.ErrMsg)
	}
	return nil
}
func (t *Tunnel) onProxySessionClose(sessionID string) error {
	logx.Debugf("Tunnel %s %s onProxySessionClose, session id: %s", t.opts.Id, t.opts.IP, sessionID)
	v, load := t.proxys.LoadAndDelete(sessionID)
	if !load {
		logx.Debugf("Tunnel %s %s onProxySessionClose, can not found session %s", t.opts.Id, t.opts.IP, sessionID)
		return nil
	}

	session := v.(*TCPProxy)
	session.closeByClient()
	return nil
}

// onProxySessionHalfClose 处理 Client 发来的半关闭请求
func (t *Tunnel) onProxySessionHalfClose(sessionID string) error {
	v, ok := t.proxys.Load(sessionID)
	if !ok {
		logx.Errorf("Tunnel.onProxySessionHalfClose not found session:%s", sessionID)
		return nil
	}

	proxy := v.(*TCPProxy)
	return proxy.closeWrite()
}

func (t *Tunnel) onProxySessionDataFromTunnel(sessionID string, data []byte, t1StartTime, t1EndTime time.Time, rawMessageSize int64) error {
	v, ok := t.proxys.Load(sessionID)
	if !ok {
		logx.Debugf("Tunnel %s %s onProxySessionDataFromTunnel, can not found session %s", t.opts.Id, t.opts.IP, sessionID)
		return nil
	}

	proxy := v.(*TCPProxy)

	// 记录 T1 统计（Client → IPPop，WebSocket 读取）
	// 使用原始 WebSocket 消息大小（包含 protobuf 编码）
	if proxy.perfStats != nil {
		t1Duration := t1EndTime.Sub(t1StartTime)
		proxy.perfStats.AddT1Read(rawMessageSize, t1Duration)
	}

	// T2 开始时间 = T1 结束时间（WebSocket 读取完成）
	// T2 包括：protobuf 解码、消息路由、查找 session
	// proxy.write() 会记录 T2 和 T3
	return proxy.write(data, t1EndTime)
}

func (t *Tunnel) onProxyTCPConnClose(sessionID string) {
	logx.Debugf("Tunnel %s %s onProxyTCPConnClose, session id: %s", t.opts.Id, t.opts.IP, sessionID)
	if _, loaded := t.proxys.LoadAndDelete(sessionID); !loaded {
		return // already closed
	}

	msg := &pb.Message{}
	msg.Type = pb.MessageType_PROXY_SESSION_CLOSE
	msg.SessionId = sessionID
	msg.Payload = nil

	buf, err := proto.Marshal(msg)
	if err != nil {
		logx.Errorf("Tunnel %s %s onProxyConnClose, EncodeMessage failed:%s", t.opts.Id, t.opts.IP, err.Error())
		return
	}

	if err = t.write(buf); err != nil {
		logx.Errorf("Tunnel %s %s onProxyConnClose, write message to tunnel failed:%s", t.opts.Id, t.opts.IP, err.Error())
	}
}

// onProxyTCPConnHalfClose 处理 SOCKS5半关闭
func (t *Tunnel) onProxyTCPConnHalfClose(sessionID string) {
	msg := &pb.Message{}
	msg.Type = pb.MessageType_PROXY_SESSION_HALF_CLOSE
	msg.SessionId = sessionID
	msg.Payload = nil

	buf, err := proto.Marshal(msg)
	if err != nil {
		logx.Errorf("Tunnel.onProxyTCPConnHalfClose, EncodeMessage failed:%s", err.Error())
		return
	}

	if err = t.write(buf); err != nil {
		logx.Errorf("Tunnel.onProxyTCPConnHalfClose, write message to tunnel failed:%s", err.Error())
	}
}

func (t *Tunnel) onProxyDataFromProxy(sessionID string, data []byte) {
	msg := &pb.Message{}
	msg.Type = pb.MessageType_PROXY_SESSION_DATA
	msg.SessionId = sessionID
	msg.Payload = data

	buf, err := proto.Marshal(msg)
	if err != nil {
		logx.Errorf("Tunnel %s %s onProxyDataFromProxy proto message failed:%s", t.opts.Id, t.opts.IP, err.Error())
		return
	}

	if err = t.write(buf); err != nil {
		logx.Errorf("Tunnel %s %s onProxyDataFromProxy, write message to tunnel failed:%s", t.opts.Id, t.opts.IP, err.Error())
	}

}

func (t *Tunnel) acceptSocks5TCPConn(conn net.Conn, targetInfo *socks5.SocksTargetInfo) error {
	logx.Debugf("acceptSocks5TCPConn, dest %s:%d", targetInfo.DomainName, targetInfo.Port)
	if t.proxys.Count() == 0 {
		now := time.Now()
		t.trafficStats.ReadStartTime.Store(&now)
	}

	now := time.Now()

	// 确定统计维度使用的目标标识（SOCKS5 库中 DomainName 会包含域名或 IP 字符串）
	targetIdentifier := targetInfo.DomainName
	if targetIdentifier == "" {
		targetIdentifier = "unknown"
	}

	sessionID := uuid.NewString()
	proxyTCP := newTCPProxy(sessionID, conn, t, targetInfo.Username, targetIdentifier, t.opts.CountryCode)

	t.proxys.Store(sessionID, proxyTCP)

	addr := fmt.Sprintf("%s:%d", targetInfo.DomainName, targetInfo.Port)
	err := t.createClientWithDest(&pb.DestAddr{Addr: addr}, sessionID)
	if err != nil {
		t.proxys.Delete(sessionID)
		return fmt.Errorf("Tunnel.acceptSocks5TCPConn client create by Domain failed, cost:%dms, addr:%s, err:%v, tun:%s ip:%s", time.Since(now).Milliseconds(), addr, err, t.opts.Id, t.opts.IP)
	}

	if len(targetInfo.ExtraBytes) > 0 {
		t.onProxyDataFromProxy(sessionID, targetInfo.ExtraBytes)
	}

	return proxyTCP.proxyConn()
}

func (t *Tunnel) createClientWithDest(dest *pb.DestAddr, sessionID string) error {
	if t.isNodeVersionGreatThanV011() {
		return t.createClientWithDestV2(dest, sessionID)
	}
	return t.createClientWithDestV1(dest, sessionID)
}

func (t *Tunnel) createClientWithDestV2(dest *pb.DestAddr, sessionID string) error {
	buf, err := proto.Marshal(dest)
	if err != nil {
		return err
	}

	msg := &pb.Message{}
	msg.Type = pb.MessageType_PROXY_SESSION_CREATE
	msg.SessionId = sessionID
	msg.Payload = buf

	data, err := proto.Marshal(msg)
	if err != nil {
		return err
	}

	return t.write(data)
}

func (t *Tunnel) createClientWithDestV1(dest *pb.DestAddr, sessionID string) error {
	logx.Debugf("Tunnel.createClientWithDestV1, session %s dest %s", sessionID, dest.Addr)

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
		logx.Debugf("Tunnel %s %s onProxyUDPDataFromTunnel session %s not exist", t.opts.Id, t.opts.IP, sessionID)
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

	logx.Debugf("Tunnel %s %s acceptSocks5UDPData new UDPProxy %s", t.opts.Id, t.opts.IP, sessionID)

	// 异步上报 UDP 会话开始 (补全监控数据，防止 ActiveSessions 计数不平衡)
	if t.tunMgr.perfCollector != nil {
		t.tunMgr.perfCollector.ReportSessionStart(udpInfo.UserName)
	}

	t.proxys.Store(sessionID, udp)
	return udp.writeToDest(data)
}

func (t *Tunnel) keepalive() {
	if t.waitPong.Load() > waitPongTimeout {
		logx.Errorf("keepalive timeout, close tunnel %s connection", t.opts.Id)
		t.close()
		return
	}

	// If waitPong is 0, it means a message was received recently (passive keepalive).
	// We can skip sending an active Ping this time and increment waitPong.
	if t.waitPong.Load() == 0 {
		t.waitPong.Add(1)
		return
	}

	b := make([]byte, 8)
	now := time.Now().UnixMicro()
	binary.LittleEndian.PutUint64(b, uint64(now))

	if err := t.writePing(b); err != nil {
		logx.Errorf("Tunnel %s %s keepalive writePing failed:%v", t.opts.Id, t.opts.IP, err.Error())
	}

	// if t.waitPong > 3 {
	// 	logx.Debugf("tunnel %s keepalive send ping, waitPong:%d, delay:%d", t.opts.Id, t.waitPong, t.delay)
	// }

	t.waitPong.Add(1)
}

func (t *Tunnel) write(msg []byte) error {
	t.writeLock.Lock()
	defer t.writeLock.Unlock()

	return t.writeMessageWithLimitRate(websocket.BinaryMessage, msg)
}

func (t *Tunnel) readMessageWithLimitRate() (int, []byte, time.Time, time.Time, error) {
	if t.closed.Load() {
		return 0, nil, time.Time{}, time.Time{}, net.ErrClosed
	}

	// 使用 NextReader 等待下一帧的到来，以此区分空闲等待时间与传输时间
	messageType, r, err := t.conn.NextReader()
	if err != nil {
		return 0, nil, time.Time{}, time.Time{}, err
	}

	// 此时 header 已收到，记录为 T1 (传输) 的开始时间
	startTime := time.Now()
	t.trafficStats.ReadStartTime.Store(&startTime)

	// 读取实际负载数据
	data, err := io.ReadAll(r)
	endTime := time.Now()
	if err != nil {
		return messageType, nil, startTime, endTime, err
	}

	t.trafficStats.ReadDuration.Add(int64(endTime.Sub(startTime)))
	t.trafficStats.ReadBytes.Add(int64(len(data)))
	t.trafficStats.DataProcessStartTime.Store(&endTime)

	t.waitPong.Store(0)

	readLimiter := t.readLimiter
	if readLimiter != nil {
		readLimiter.WaitN(context.TODO(), len(data))
		// 将限速等待时间也计入 T1 总耗时（反映出真实的用户感知速度）
		endTime = time.Now()
	}

	return messageType, data, startTime, endTime, nil
}

func (t *Tunnel) writeMessageWithLimitRate(messageType int, data []byte) error {
	if t.closed.Load() {
		return net.ErrClosed
	}

	writeLimiter := t.writeLimiter
	if writeLimiter != nil {
		writeLimiter.WaitN(context.TODO(), len(data))
	}

	return t.conn.WriteMessage(messageType, data)
}

func (t *Tunnel) waitClose() {
	t.close()
	<-t.ctx.Done()
	logx.Debugf("tunnel %s wait close", t.opts.Id)
}

func (t *Tunnel) close() {
	if t.closed.Swap(true) {
		return
	}
	t.conn.Close()
	t.clearProxys()
}

func (t *Tunnel) clearProxys() {
	count := t.proxys.Count()
	if count > 0 {
		logx.Infof("Tunnel %s %s disconnected, cleaning up %d proxies", t.opts.Id, t.opts.IP, count)
	}

	t.proxys.Range(func(key, value any) bool {
		// Try TCPProxy
		if session, ok := value.(*TCPProxy); ok {
			logx.Errorf("Tunnel %s %s disconnected, closing TCPProxy session %s for user %s", t.opts.Id, t.opts.IP, session.id, session.userName)
			session.close()
			return true
		}

		// Try UDPProxy
		if session, ok := value.(*UDPProxy); ok {
			logx.Errorf("Tunnel %s %s disconnected, closing UDPProxy session %s for user %s", t.opts.Id, t.opts.IP, session.id, session.udpInfo.UserName)
			session.stop()
			return true
		}

		return true
	})
	t.proxys.Clear()
}

func (t *Tunnel) getTrafficStats() *TrafficStats {
	return t.trafficStats
}

func (t *Tunnel) addTrafficStats(writeBytes int, writeDuration time.Duration) {
	t.trafficStats.WriteBytes.Add(int64(writeBytes))
	t.trafficStats.WriteDuration.Add(int64(writeDuration))

	if startTime := t.trafficStats.DataProcessStartTime.Load(); startTime != nil {
		duration := time.Since(*startTime) - writeDuration
		t.trafficStats.DataProcessDuration.Add(int64(duration))
	}
}

// great than 0.1.1
func (t *Tunnel) isNodeVersionGreatThanV011() bool {
	return semver.Compare("v"+t.opts.Version, "v0.1.1") > 0
}
