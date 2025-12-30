package ws

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"errors"
	"fmt"
	"math/rand"
	"net"
	"sync"
	"sync/atomic"
	"time"
	"titan-ipoverlay/ippop/config"
	"titan-ipoverlay/ippop/model"
	"titan-ipoverlay/ippop/socks5"

	"github.com/bluele/gcache"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/stores/redis"
)

const (
	userCacheSize = 512
	// 30 seconds
	keepaliveInterval         = 30
	userTrafficSaveInterval   = 300
	setOnlineTableExpireTick  = 90
	onlineTableExpireTime     = 2 * setOnlineTableExpireTick
	userSessionExpireInterval = 120
	userSessionExpireDuration = 3 * time.Minute
)

type UserSession struct {
	deviceID     string
	sessTime     int64
	assignedTime time.Time
	// lastUsed       time.Time
	connectCount   int32
	disconnectTime time.Time
}

type ExpiredSession struct {
	username      string
	sessionID     string
	deviceID      string
	isLeaseDevice bool
}

type TunnelManager struct {
	tunnels     sync.Map
	redis       *redis.Redis
	config      config.Config
	userTraffic *userTraffic
	userCache   gcache.Cache
	// userTunLock     sync.Mutex
	filterRules     *Rules
	userSessionMap  map[string]map[string]*UserSession
	userSessionLock sync.Mutex

	socks5ConnCount atomic.Int64

	tunnelListLock sync.RWMutex
	tunnelList     []*Tunnel
	rrIdx          uint64

	rng *rand.Rand

	HealthStatsMap sync.Map
}

func NewTunnelManager(config config.Config, redis *redis.Redis) *TunnelManager {
	if err := model.DeleteNodeOnlineData(context.TODO(), redis); err != nil {
		panic(err)
	}

	tm := &TunnelManager{
		config:          config,
		redis:           redis,
		userTraffic:     newUserTraffic(),
		userCache:       gcache.New(userCacheSize).LRU().Build(),
		filterRules:     &Rules{rules: RulesToMap(config.FilterRules.Rules), defaultAction: config.FilterRules.DefaultAction},
		userSessionMap:  make(map[string]map[string]*UserSession),
		userSessionLock: sync.Mutex{},
		rng:             rand.New(rand.NewSource(time.Now().UnixNano())),
	}

	routeScheduler := newUserRouteScheduler(tm)

	go tm.keepalive()
	go tm.setNodeOnlineDataExpire()
	go tm.startUserTrafficTimer()
	go tm.RecyclUserSession()
	go routeScheduler.start()
	return tm
}

// 添加 tunnel
func (tm *TunnelManager) addTunnel(t *Tunnel) {
	tm.tunnelListLock.Lock()
	defer tm.tunnelListLock.Unlock()

	tm.tunnelList = append(tm.tunnelList, t)
	atomic.StoreUint64(&tm.rrIdx, tm.rrIdx%uint64(len(tm.tunnelList)))
}

// 删除 tunnel
func (tm *TunnelManager) removeTunnel(id string) {
	tm.tunnelListLock.Lock()
	defer tm.tunnelListLock.Unlock()
	for i, t := range tm.tunnelList {
		if t.opts.Id == id {
			tm.tunnelList = append(tm.tunnelList[:i], tm.tunnelList[i+1:]...)

			if len(tm.tunnelList) > 0 {
				atomic.StoreUint64(&tm.rrIdx, tm.rrIdx%uint64(len(tm.tunnelList)))
			} else {
				atomic.StoreUint64(&tm.rrIdx, 0)
			}
			return
		}
	}
}

func (tm *TunnelManager) acceptWebsocket(conn *websocket.Conn, req *NodeWSReq, nodeIP string) {
	v, ok := tm.tunnels.Load(req.NodeId)
	if ok {
		oldTun := v.(*Tunnel)
		oldTun.waitClose()
	}

	logx.Debugf("TunnelManager.acceptWebsocket node id %s", req.NodeId)

	node, err := model.GetNode(context.TODO(), tm.redis, req.NodeId)
	if err != nil {
		logx.Errorf("TunnelManager.acceptWebsocket, get node %s", err.Error())
		return
	}

	if node == nil {
		node = &model.Node{Id: req.NodeId, RegisterAt: time.Now().Format(model.TimeLayout)}
	}

	node.OS = req.OS
	node.Version = req.Version
	node.IP = nodeIP
	node.Online = true
	node.LoginAt = time.Now().Format(model.TimeLayout)

	config := tm.config
	opts := &TunOptions{
		Id:                node.Id,
		OS:                node.OS,
		IP:                node.IP,
		UDPTimeout:        int(config.Socks5.UDPTimeout),
		TCPTimeout:        int(config.Socks5.TCPTimeout),
		DownloadRateLimti: config.WS.DownloadRateLimit,
		UploadRateLimit:   config.WS.UploadRateLimit,
	}

	tun := newTunnel(conn, tm, opts)
	tm.tunnels.Store(node.Id, tun)
	tm.HealthStatsMap.LoadOrStore(node.Id, &HealthStats{})

	tm.addTunnel(tun)

	defer tun.leaseComplete()

	defer tm.tunnels.Delete(node.Id)
	defer tm.removeTunnel(node.Id)

	if err := model.HandleNodeOnline(context.Background(), tm.redis, node); err != nil {
		logx.Errorf("HandleNodeOnline:%s", err.Error())
		return
	}

	defer tm.handleNodeOffline(node.Id)

	tun.serve()
}

func (tm *TunnelManager) handleNodeOffline(nodeID string) {
	if err := model.SetNodeOffline(tm.redis, nodeID); err != nil {
		logx.Errorf("handleNodeOffline SetNodeOffline %v", err)
	}

	node, err := model.GetNode(context.TODO(), tm.redis, nodeID)
	if err != nil {
		logx.Errorf("handleNodeOffline GetNode %v", err)
		return
	}

	if node == nil {
		logx.Errorf("handleNodeOffline node == nil, id:%s", nodeID)
		return
	}

	if len(node.BindUser) > 0 {
		user, err := model.GetUser(tm.redis, node.BindUser)
		if err != nil {
			logx.Errorf("handleNodeOffline, get user %s for node %s faild:%v", node.BindUser, node.Id, err)
			return
		}

		if user == nil {
			logx.Errorf("handleNodeOffline, get user %s for node %s, but user not exist", node.BindUser, node.Id)
			return
		}

		if user.RouteMode != int(model.RouteModeAuto) {
			logx.Alert(fmt.Sprintf("handleNodeOffline, node %s is offline, but bind by user %s with mode=%s", node.Id, user.UserName, model.RouteMode(user.RouteMode)))
			return
		}

		if user.RouteNodeID != node.Id {
			logx.Errorf("handleNodeOffline, get user %s for node %s, but user.RouteNodeID[%s] != node.id", node.BindUser, node.Id, user.RouteNodeID)
			return
		}

		logx.Debugf("node %s offline, user %s trigger siwth node", node.Id, node.BindUser)
		if err := tm.switchNodeForUser(user); err != nil {
			logx.Errorf("handleNodeOffline switchNodeForUser %s failed: %v", node.BindUser, err)
		}
	} else {
		if err := model.RemoveFreeNode(tm.redis, nodeID); err != nil {
			logx.Errorf("handleNodeOffline RemoveFreeNode %v", err)
		}
	}

}

func (tm *TunnelManager) switchNodeForUser(user *model.User) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	newNodeID, err := model.AllocateFreeNode(ctx, tm.redis)
	if err != nil {
		return err
	}

	if err := model.SwitchNodeByUser(ctx, tm.redis, user, string(newNodeID)); err != nil {
		if err2 := model.AddFreeNode(tm.redis, string(newNodeID)); err2 != nil {
			logx.Errorf("switchNodeForUser AddFreeNode: %v", err2)
		}
		return err
	}

	tm.DeleteUserFromCache(user.UserName)

	return nil
}

func (tm *TunnelManager) getUserFromCache(userName string) (*model.User, error) {
	v, err := tm.userCache.Get(userName)
	if err != nil {
		if !errors.Is(err, gcache.KeyNotFoundError) {
			return nil, err
		}

		user, err := model.GetUser(tm.redis, userName)
		if err != nil {
			return nil, err
		}

		if user == nil {
			return nil, fmt.Errorf("user %s not exist", userName)
		}

		tm.userCache.Set(userName, user)

		return user, nil
	}
	// logx.Debugf("getUserFromCache v:%v", v)
	return v.(*model.User), nil
}

func (tm *TunnelManager) DeleteUserFromCache(userName string) {
	tm.userCache.Remove(userName)
}

func (tm *TunnelManager) KickNode(nodeID string) error {
	v, ok := tm.tunnels.Load(nodeID)
	if ok {
		tun := v.(*Tunnel)
		tun.waitClose()
		return nil
	}

	return fmt.Errorf("node %s already offline", nodeID)
}

func (tm *TunnelManager) allocateTunnelByUserSession(user *model.User, sessionID string, sessTime int64) (*Tunnel, *UserSession, error) {
	tm.userSessionLock.Lock()
	defer tm.userSessionLock.Unlock()

	// No sessionID provided → return a random tunnel
	if sessionID == "" {
		tun, err := tm.randomTunnel()
		if err != nil {
			return nil, nil, err
		}
		return tun, nil, nil
	}

	// Load or create the session map for this user
	sessions, ok := tm.userSessionMap[user.UserName]
	if !ok {
		sessions = make(map[string]*UserSession)
		tm.userSessionMap[user.UserName] = sessions
	}

	// If the session already exists, try to reuse it
	if session, exists := sessions[sessionID]; exists {
		// Look up the tunnel by the deviceID bound to this session
		if v, ok := tm.tunnels.Load(session.deviceID); ok {
			session.connectCount++
			return v.(*Tunnel), session, nil
		}

		// Session exists but its tunnel does not (unexpected state)
		// → fall through to allocate a new session
	}

	// Allocate a new session by taking a free device node
	nodeIDBytes, err := model.AllocateFreeNode(context.Background(), tm.redis)
	if err != nil {
		return nil, nil, err
	}

	// Create the session object if it does not exist yet
	session := sessions[sessionID]
	if session == nil {
		session = &UserSession{
			sessTime: sessTime,
		}
		sessions[sessionID] = session
	}

	session.deviceID = string(nodeIDBytes)
	session.assignedTime = time.Now()
	session.connectCount++

	// Load the corresponding tunnel instance
	v, ok := tm.tunnels.Load(session.deviceID)
	if !ok {
		return nil, nil, fmt.Errorf("allocated deviceID=%s but tunnel not found", session.deviceID)
	}

	tunnel := v.(*Tunnel)
	tunnel.userSessionID = sessionID
	tunnel.setRateLimit(user.DownloadRateLimit, user.UploadRateLimit)

	return tunnel, session, nil
}

func (tm *TunnelManager) getTunnelByUser(user *model.User) (*Tunnel, error) {
	v, ok := tm.tunnels.Load(user.RouteNodeID)
	if !ok {
		return nil, nil
	}
	tun := v.(*Tunnel)

	tun.setRateLimit(user.DownloadRateLimit, user.UploadRateLimit)

	return tun, nil
}

func (tm *TunnelManager) randomTunnel() (*Tunnel, error) {
	tm.tunnelListLock.RLock()
	defer tm.tunnelListLock.RUnlock()

	n := len(tm.tunnelList)
	if n == 0 {
		return nil, fmt.Errorf("no tunnel exist")
	}

	for i := 0; i < n; i++ {
		idx := tm.rng.Intn(n)
		tun := tm.tunnelList[idx]

		v, ok := tm.HealthStatsMap.Load(tun.opts.Id)
		if ok {
			if v.(*HealthStats).checkValid() {
				return tun, nil
			} else {
				logx.Errorf("node %s is invalid", tun.opts.Id)
			}
		}
	}

	return nil, fmt.Errorf("all tunnel invalid")

}

func (tm *TunnelManager) nextTunnel() (*Tunnel, error) {
	tm.tunnelListLock.RLock()
	defer tm.tunnelListLock.RUnlock()

	n := len(tm.tunnelList)
	if n == 0 {
		return nil, fmt.Errorf("no tunnel exist")
	}

	idx := atomic.AddUint64(&tm.rrIdx, 1)
	return tm.tunnelList[idx%uint64(n)], nil
}

func (tm *TunnelManager) handleUserSessionWhenSocks5TCPClose(session *UserSession) {
	tm.userSessionLock.Lock()
	defer tm.userSessionLock.Unlock()

	session.connectCount--
	session.disconnectTime = time.Now()
}

func (tm *TunnelManager) HandleSocks5TCP(tcpConn *net.TCPConn, targetInfo *socks5.SocksTargetInfo) error {
	logx.Debugf("HandleSocks5TCP, user %s, DomainName %s, port %d, remote:%s, connCount:%d, connTime:%d",
		targetInfo.Username, targetInfo.DomainName, targetInfo.Port, tcpConn.RemoteAddr().String(), tm.socks5ConnCount.Load(), time.Since(targetInfo.ConnCreateTime).Milliseconds())

	tm.socks5ConnCount.Add(1)
	defer tm.socks5ConnCount.Add(-1)

	if tm.filterRules.isDeny(targetInfo.DomainName, fmt.Sprintf("%d", targetInfo.Port)) {
		return fmt.Errorf("tcp: target %s:%d have been deny", targetInfo.DomainName, targetInfo.Port)
	}

	user, err := tm.getUserFromCache(targetInfo.Username)
	if err != nil {
		return err
	}

	var tun *Tunnel
	var userSession *UserSession
	if user.RouteMode == int(model.RouteModeCustom) {
		tun, userSession, err = tm.allocateTunnelByUserSession(user, targetInfo.Session, targetInfo.SessTime)
	} else {
		tun, err = tm.getTunnelByUser(user)
	}

	if err != nil {
		return err
	}

	if tun == nil {
		return fmt.Errorf("can not allocate tunnel, user %s", targetInfo.Username)
	}

	logx.Debugf("allocate tun %s for user %s session %s", tun.opts.Id, targetInfo.Username, targetInfo.Session)

	defer func() {
		if userSession != nil {
			tm.handleUserSessionWhenSocks5TCPClose(userSession)
		}
	}()

	return tun.acceptSocks5TCPConn(tcpConn, targetInfo)
}

func (tm *TunnelManager) HandleSocks5UDP(udpConn socks5.UDPConn, udpInfo *socks5.Socks5UDPInfo, data []byte) error {
	host, port, err := net.SplitHostPort(udpInfo.Dest)
	if err != nil {
		return err
	}

	if tm.filterRules.isDeny(host, port) {
		return fmt.Errorf("udp: target %s have been deny", udpInfo.Dest)
	}

	user, err := tm.getUserFromCache(udpInfo.UserName)
	if err != nil {
		return err
	}

	tun, err := tm.getTunnelByUser(user)
	if err != nil {
		return err
	}
	if tun == nil {
		return fmt.Errorf("can not allocate tunnel, user %s", udpInfo.UserName)
	}

	return tun.acceptSocks5UDPData(udpConn, udpInfo, data)
}

func (tm *TunnelManager) HandleUserAuth(userName, password string) error {
	logx.Debugf("HandleUserAuth username %s password %s", userName, password)
	user, err := tm.getUserFromCache(userName)
	if err != nil {
		return fmt.Errorf("get user from redis error %v", err)
	}

	if user == nil {
		return fmt.Errorf("user %s not exist", userName)
	}

	if user.Off {
		return fmt.Errorf("user %s off", userName)
	}

	hash := md5.Sum([]byte(password))
	passwordMD5 := hex.EncodeToString(hash[:])
	if user.PasswordMD5 != passwordMD5 {
		return fmt.Errorf("password not match")
	}

	now := time.Now().Unix()
	if now < user.StartTime || now > user.EndTime {
		startTime := time.Unix(user.StartTime, 0)
		endTime := time.Unix(user.EndTime, 0)
		return fmt.Errorf("user %s is out of date[%s~%s]", userName, startTime.Format(time.RFC3339), endTime.Format(time.RFC3339))
	}

	if (user.TotalTraffic != 0) && (user.CurrentTraffic >= user.TotalTraffic) {
		return fmt.Errorf("user %s is out of traffic %d, currentTraffic %d", user.UserName, user.TotalTraffic, user.CurrentTraffic)
	}

	return nil
}

func (tm *TunnelManager) keepalive() {
	tick := 0
	for {
		time.Sleep(time.Second * 1)
		tick++

		if tick == keepaliveInterval {
			tick = 0
			count := 0
			failureCount := 0
			now := time.Now()
			tm.tunnels.Range(func(key, value any) bool {
				t := value.(*Tunnel)
				t.keepalive()

				v, ok := tm.HealthStatsMap.Load(key.(string))
				if ok {
					healthStats := v.(*HealthStats)
					if healthStats.isInvalid() {
						failureCount++
					}
				}

				count++
				return true
			})

			logx.Debugf("TunnelManager.keepalive tunnel count:%d, cost:%v, failureCount:%d", count, time.Since(now), failureCount)
		}
	}
}

func (tm *TunnelManager) setNodeOnlineDataExpire() {
	ticker := time.NewTicker(setOnlineTableExpireTick * time.Second)
	defer ticker.Stop()

	for {
		<-ticker.C
		model.SetNodeOnlineDataExpire(context.TODO(), tm.redis, onlineTableExpireTime)
	}
}

func (tm *TunnelManager) startUserTrafficTimer() {
	ticker := time.NewTicker(userTrafficSaveInterval * time.Second)
	defer ticker.Stop()

	for {
		<-ticker.C
		trafficMap := tm.userTraffic.snapshotAndClear()
		if err := model.AddUsersTrafficOneDay(context.TODO(), tm.redis, trafficMap); err != nil {
			logx.Errorf("AddUsersDayTraffic failed:%v", err)
		}

		if err := model.AddUsersTrafficOneHour(context.TODO(), tm.redis, trafficMap); err != nil {
			logx.Errorf("AddUsersDayTraffic failed:%v", err)
		}

		if err := model.AddUsersTraffic5Minutes(context.TODO(), tm.redis, trafficMap); err != nil {
			logx.Errorf("AddUsersDayTraffic failed:%v", err)
		}

		if err := model.AddUsersTotalTraffic(context.TODO(), tm.redis, trafficMap); err != nil {
			logx.Errorf("AddUsersTotalTraffic failed:%v", err)
		}

	}
}

func (tm *TunnelManager) traffic(userName string, traffic int64) {
	tm.userTraffic.add(userName, traffic)
}

func (tm *TunnelManager) RecyclUserSession() {
	ticker := time.NewTicker(userSessionExpireInterval * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		tm.userSessionLock.Lock()

		removeSessions := tm.collectExpiredSessions()

		tm.removeSessions(removeSessions)

		tm.userSessionLock.Unlock()
	}
}

func (tm *TunnelManager) collectExpiredSessions() map[string]*ExpiredSession {
	expiredSessions := make(map[string]*ExpiredSession)

	for username, userSessions := range tm.userSessionMap {
		logx.Debugf("user %s session count %d", username, len(userSessions))
		for sessionID, session := range userSessions {
			if session.connectCount > 0 {
				logx.Debugf("session %s connectCount %d", sessionID, session.connectCount)
				continue
			}

			if time.Since(session.disconnectTime) <= userSessionExpireDuration {
				// logx.Debugf("session %s is free later %fs  count %d", sessionID, (userSessionExpireDuration - time.Since(session.disconnectTime)).Seconds(), session.connectCount)
				continue
			}

			expiredSession := &ExpiredSession{
				sessionID: sessionID,
				deviceID:  session.deviceID,
				username:  username,
			}

			if v, ok := tm.tunnels.Load(session.deviceID); ok {
				tunnel := v.(*Tunnel)
				expiredSession.isLeaseDevice = tunnel.userSessionID == sessionID
			}

			expiredSessions[uuid.NewString()] = expiredSession
		}
	}

	return expiredSessions
}

func (tm *TunnelManager) removeSessions(expiredSessions map[string]*ExpiredSession) {
	// logx.Debugf("remove expire session count %d", len(expiredSessions))
	for _, sess := range expiredSessions {
		// logx.Debugf("remove session %#v", sess)
		sessions := tm.userSessionMap[sess.username]
		delete(sessions, sess.sessionID)

		if len(sessions) == 0 {
			delete(tm.userSessionMap, sess.username)
		}

		if sess.isLeaseDevice {
			model.AddFreeNode(tm.redis, sess.deviceID)
		}
	}
}

// implement interface for rpc server
/*type NodeManager interface {
	Kick(nodeID string) error
}

type UserManager interface {
	SwitchNode(userName string) error
	DeleteCache(userName string) error
}

type EndpointProvider interface {
	GetAuth() (secret string, expire int64, err error)
	GetWSURL() (string, error)
	GetSocks5Addr() (string, error)
}*/

func (tm *TunnelManager) Kick(nodeID string) error {
	return tm.KickNode(nodeID)
}
func (tm *TunnelManager) SwitchNode(userName string) error {
	user, err := model.GetUser(tm.redis, userName)
	if err != nil {
		return err
	}

	if user == nil {
		return fmt.Errorf("handleNodeOffline, user %s not exist", userName)
	}

	return tm.switchNodeForUser(user)
}

func (tm *TunnelManager) DeleteCache(userName string) error {
	tm.DeleteUserFromCache(userName)
	return nil
}

func (tm *TunnelManager) GetAuth() (secret string, expire int64, err error) {
	return tm.config.JwtAuth.AccessSecret, tm.config.JwtAuth.AccessExpire, nil
}
func (tm *TunnelManager) GetWSURL() (string, error) {
	domain := tm.config.Socks5.ServerIP
	if len(tm.config.WS.Domain) > 0 {
		domain = tm.config.WS.Domain
	}
	return fmt.Sprintf("ws://%s:%d/ws/node", domain, tm.config.WS.Port), nil
}
func (tm *TunnelManager) GetSocks5Addr() (string, error) {
	_, port, err := net.SplitHostPort(tm.config.Socks5.Addr)
	if err != nil {
		return "", err
	}
	socks5Addr := fmt.Sprintf("%s:%s", tm.config.Socks5.ServerIP, port)
	return socks5Addr, nil
}
