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
	"github.com/gorilla/websocket"
	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/stores/redis"
)

const (
	userCacheSize = 512
	// 30 seconds
	keepaliveInterval         = 2
	userTrafficSaveInterval   = 300
	tunnelTrafficSaveInterval = 10

	setOnlineTableExpireTick  = 90
	onlineTableExpireTime     = 2 * setOnlineTableExpireTick
	userSessionExpireInterval = 120
	userSessionExpireDuration = 3 * time.Minute
)

// UserSession and ExpiredSession are now handled by the allocator module.

type TunnelManager struct {
	tunnels           sync.Map
	redis             *redis.Redis
	config            config.Config
	userTraffic       *userTraffic
	userCache         gcache.Cache
	allocatorRegistry *AllocatorRegistry
	sessionManager    *SessionManager

	filterRules     *Rules
	userSessionMap  map[string]map[string]*UserSession
	userSessionLock sync.Mutex

	socks5ConnCount atomic.Int64

	tunnelListLock sync.RWMutex
	tunnelList     []*Tunnel
	rrIdx          uint64

	rng *rand.Rand

	ipBlacklist sync.Map

	// HealthStatsMap sync.Map
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

		tunnelList: make([]*Tunnel, 0, 100000),
	}

	tm.sessionManager = NewSessionManager(tm, userSessionExpireDuration)
	tm.allocatorRegistry = NewAllocatorRegistry()
	tm.allocatorRegistry.Register(model.RouteModeAuto, NewStaticAllocator(tm))
	tm.allocatorRegistry.Register(model.RouteModeManual, NewStaticAllocator(tm))
	tm.allocatorRegistry.Register(model.RouteModeTimed, NewStaticAllocator(tm))
	tm.allocatorRegistry.Register(model.RouteModeCustom, NewSessionAllocator(tm.sessionManager, tm))

	tm.loadBlacklist()

	go tm.keepalive()
	go tm.setNodeOnlineDataExpire()
	go tm.startUserTrafficTimer()
	go tm.startTunnelTrafficTimer()
	return tm
}

func (tm *TunnelManager) loadBlacklist() {
	ips, err := model.GetBlacklist(tm.redis)
	if err != nil {
		logx.Errorf("loadBlacklist error: %v", err)
		return
	}

	for _, ip := range ips {
		tm.ipBlacklist.Store(ip, struct{}{})
	}
	logx.Infof("loadBlacklist success, count: %d", len(ips))
}

// 添加 tunnel
func (tm *TunnelManager) addTunnel(t *Tunnel) {
	if t.opts.IsBlacklisted {
		logx.Infof("addTunnel error: %s %s is in blacklist", t.opts.Id, t.opts.IP)
		return
	}

	tm.tunnelListLock.Lock()
	defer tm.tunnelListLock.Unlock()

	t.index = len(tm.tunnelList)
	tm.tunnelList = append(tm.tunnelList, t)

	rrIdx := atomic.LoadUint64(&tm.rrIdx)
	atomic.StoreUint64(&tm.rrIdx, rrIdx%uint64(len(tm.tunnelList)))
}

// 删除 tunnel
func (tm *TunnelManager) removeTunnel(tun *Tunnel) {
	if tun.opts.IsBlacklisted {
		logx.Infof("removeTunnel error: %s %s is in blacklist", tun.opts.Id, tun.opts.IP)
		return
	}

	tm.tunnelListLock.Lock()
	defer tm.tunnelListLock.Unlock()

	idx := tun.index
	last := len(tm.tunnelList) - 1
	if idx < 0 || idx > last {
		return
	}

	// 如果不是最后一个，交换
	if idx != last {
		lastTunnel := tm.tunnelList[last]
		tm.tunnelList[idx] = lastTunnel
		lastTunnel.index = idx
	}

	// 删除最后一个
	tm.tunnelList[last] = nil
	tm.tunnelList = tm.tunnelList[:last]

	// 标记已移除（防止重复 remove）
	tun.index = -1

	if len(tm.tunnelList) > 0 {
		atomic.StoreUint64(&tm.rrIdx, tm.rrIdx%uint64(len(tm.tunnelList)))
	} else {
		atomic.StoreUint64(&tm.rrIdx, 0)
	}
}

func (tm *TunnelManager) acceptWebsocket(conn *websocket.Conn, req *NodeWSReq, nodeIP string) {
	v, ok := tm.tunnels.Load(req.NodeId)
	if ok {
		oldTun := v.(*Tunnel)
		logx.Infof("TunnelManager.acceptWebsocket force close tunnel %s ip %s", req.NodeId, nodeIP)
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

	if _, ok := tm.ipBlacklist.Load(nodeIP); ok {
		node.IsBlacklisted = true
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
		Version:           node.Version,
		UDPTimeout:        int(config.Socks5.UDPTimeout),
		TCPTimeout:        int(config.Socks5.TCPTimeout),
		DownloadRateLimti: config.WS.DownloadRateLimit,
		UploadRateLimit:   config.WS.UploadRateLimit,
		IsBlacklisted:     node.IsBlacklisted,
	}

	tun := newTunnel(conn, tm, opts)
	tm.tunnels.Store(node.Id, tun)
	// tm.HealthStatsMap.LoadOrStore(node.Id, &HealthStats{})

	tm.addTunnel(tun)

	defer tun.leaseComplete()

	defer tm.tunnels.Delete(node.Id)
	defer tm.removeTunnel(tun)

	if err := model.HandleNodeOnline(context.Background(), tm.redis, node); err != nil {
		logx.Errorf("HandleNodeOnline:%s", err.Error())
		return
	}

	defer model.HandleNodeOffline(context.Background(), tm.redis, node)

	tun.serve()
}

func (tm *TunnelManager) SwitchNodeForUser(user *model.User) error {
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
	return v.(*model.User), nil
}

func (tm *TunnelManager) DeleteUserFromCache(userName string) {
	tm.userCache.Remove(userName)
}

func (tm *TunnelManager) KickNode(nodeID string) error {
	v, ok := tm.tunnels.Load(nodeID)
	if ok {
		tun := v.(*Tunnel)
		logx.Errorf("kick node %s, ip %s", nodeID, tun.opts.IP)
		tun.waitClose()
		return nil
	}

	return fmt.Errorf("node %s already offline", nodeID)
}

// allocateTunnelByUserSession, randomTunnel, nextTunnel are now handled by NodeSource implementation or Allocators.

func (tm *TunnelManager) getTunnelByUser(user *model.User) (*Tunnel, error) {
	v, ok := tm.tunnels.Load(user.RouteNodeID)
	if !ok {
		return nil, nil
	}
	tun := v.(*Tunnel)

	tun.setRateLimit(user.DownloadRateLimit, user.UploadRateLimit)

	return tun, nil
}

// NodeSource Interface Implementation

func (tm *TunnelManager) AcquireExclusiveNode(ctx context.Context) (*Tunnel, error) {
	nodeIDBytes, err := model.AllocateFreeNode(ctx, tm.redis)
	if err != nil {
		return nil, err
	}
	nodeID := string(nodeIDBytes)
	v, ok := tm.tunnels.Load(nodeID)
	if !ok {
		// If tunnel is not online locally, we don't put it back to free pool
		// because the free pool should only contain online/available nodes.
		return nil, fmt.Errorf("node %s allocated but not found in local tunnels", nodeID)
	}
	return v.(*Tunnel), nil
}

func (tm *TunnelManager) ReleaseExclusiveNodes(nodeIDs []string) {
	// Only release nodes that are still online locally
	onlineNodes := make([]string, 0, len(nodeIDs))
	for _, id := range nodeIDs {
		if _, ok := tm.tunnels.Load(id); ok {
			onlineNodes = append(onlineNodes, id)
		}
	}

	if len(onlineNodes) == 0 {
		return
	}

	err := model.AddFreeNodes(context.Background(), tm.redis, onlineNodes)
	if err != nil {
		logx.Errorf("ReleaseExclusiveNodes failed: %v", err)
	}
}

func (tm *TunnelManager) GetLocalTunnel(nodeID string) *Tunnel {
	v, ok := tm.tunnels.Load(nodeID)
	if !ok {
		return nil
	}
	return v.(*Tunnel)
}

func (tm *TunnelManager) PickActiveTunnel() (*Tunnel, error) {
	switch tm.config.WS.TunnelSelectPolicy {
	case config.TunnelSelectRandom:
		return tm.randomTunnel()
	case config.TunnelSelectRound:
		return tm.nextTunnel()
	default:
		return tm.randomTunnel()
	}
}

func (tm *TunnelManager) randomTunnel() (*Tunnel, error) {
	tm.tunnelListLock.RLock()
	defer tm.tunnelListLock.RUnlock()

	n := len(tm.tunnelList)
	if n == 0 {
		return nil, fmt.Errorf("no tunnel exist")
	}

	idx := tm.rng.Intn(n)
	return tm.tunnelList[idx], nil
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
	tm.sessionManager.Decrement(session)
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

	mode := model.RouteMode(user.RouteMode)
	allocator := tm.allocatorRegistry.Get(mode)
	if allocator == nil {
		return fmt.Errorf("no allocator for mode %v", mode)
	}

	tun, userSession, err := allocator.Allocate(user, targetInfo)
	if err != nil {
		return err
	}

	if tun == nil {
		return fmt.Errorf("can not allocate tunnel, user %s", targetInfo.Username)
	}

	logx.Debugf("allocate tun %s for user %s session %s", tun.opts.Id, targetInfo.Username, targetInfo.Session)

	if userSession != nil {
		defer tm.sessionManager.Decrement(userSession)
	}

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
	logx.Debugf("HandleUserAuth username %s", userName)
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
	const workerCount = 16
	taskCh := make(chan *Tunnel, workerCount*2)
	for i := 0; i < workerCount; i++ {
		go func() {
			for t := range taskCh {
				t.keepalive()
			}
		}()
	}

	ticker := time.NewTicker(time.Second * keepaliveInterval)
	defer ticker.Stop()

	tickCount := 0
	for range ticker.C {
		now := time.Now()
		count := 0
		tm.tunnels.Range(func(key, value any) bool {
			t := value.(*Tunnel)
			taskCh <- t
			count++
			return true
		})

		tickCount++
		if tickCount > 10 {
			logx.Infof("TunnelManager.keepalive tunnel count:%d, cost:%v", count, time.Since(now))
			tickCount = 0
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

func (tm *TunnelManager) startTunnelTrafficTimer() {
	ticker := time.NewTicker(tunnelTrafficSaveInterval * time.Second)
	defer ticker.Stop()

	for {
		<-ticker.C
	}
}

// func (tm *TunnelManager) traffic(userName string, traffic int64) {
// 	tm.userTraffic.add(userName, traffic)
// }

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

	return tm.SwitchNodeForUser(user)
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
