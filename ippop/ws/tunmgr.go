package ws

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"titan-ipoverlay/ippop/config"
	"titan-ipoverlay/ippop/metrics"
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
	acceptLockShards          = 16384
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

	filterRules *Rules

	socks5ConnCount atomic.Int64

	ipBlacklist sync.Map

	uploadTestResults sync.Map // key: nodeID, value: *pb.UploadTestResult
	uploadTestTotal   atomic.Int32

	// HealthStatsMap sync.Map
	ipPool *IPPool

	acceptLocks []sync.Mutex
	// 会话性能数据收集器
	perfCollector *SessionPerfCollector
}

func NewTunnelManager(config config.Config, redis *redis.Redis) *TunnelManager {
	if err := model.DeleteNodeOnlineData(context.TODO(), redis); err != nil {
		panic(err)
	}

	tm := &TunnelManager{
		config:      config,
		redis:       redis,
		userTraffic: newUserTraffic(),
		userCache:   gcache.New(userCacheSize).LRU().Build(),
		filterRules: &Rules{rules: RulesToMap(config.FilterRules.Rules), defaultAction: config.FilterRules.DefaultAction},

		ipPool:        NewIPPool(),
		acceptLocks:   make([]sync.Mutex, acceptLockShards),
		perfCollector: NewSessionPerfCollector(config.ClickHouse, config.GetNodeID()),
	}

	tm.sessionManager = NewSessionManager(tm, userSessionExpireDuration)
	tm.allocatorRegistry = NewAllocatorRegistry()
	tm.allocatorRegistry.Register(model.RouteModeAuto, NewStaticAllocator(tm))
	tm.allocatorRegistry.Register(model.RouteModeManual, NewStaticAllocator(tm))
	tm.allocatorRegistry.Register(model.RouteModeTimed, NewStaticAllocator(tm))
	tm.allocatorRegistry.Register(model.RouteModeCustom, NewSessionAllocator(tm.sessionManager, tm))
	tm.allocatorRegistry.Register(model.RouteModePolling, NewPollingAllocator(tm))

	tm.loadBlacklist()

	go tm.keepalive()
	go tm.setNodeOnlineDataExpire()
	if tm.config.TrafficStats.EnableUserTraffic {
		go tm.startUserTrafficTimer()
	}
	go tm.perfCollector.Start()
	go tm.syncBlacklistLoop()
	return tm
}

func (tm *TunnelManager) syncBlacklistLoop() {
	if !tm.config.QoS.EnableBandwidthBlacklist {
		return
	}

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		tm.loadBlacklist()
	}
}

func (tm *TunnelManager) loadBlacklist() {
	// 1. 获取所有过期的假释 IP，并从黑名单和 ZSET 中原子移除 (O(1) 网络开销)
	expiredIPs, err := model.ProcessExpiredProbations(tm.redis)
	if err != nil {
		logx.Errorf("QoS: Failed to process expired probations: %v", err)
	}
	if len(expiredIPs) > 0 {
		logx.Infof("QoS: Probation period ended for IPs %v, removed from Redis SET", expiredIPs)
	}

	ips, err := model.GetBlacklist(tm.redis)
	if err != nil {
		logx.Errorf("loadBlacklist error: %v", err)
		return
	}

	currentIPs := make(map[string]struct{})
	for _, ip := range ips {
		currentIPs[ip] = struct{}{}
	}

	// 移除本地已解封的 IP
	tm.ipBlacklist.Range(func(key, value interface{}) bool {
		ip := key.(string)
		if _, ok := currentIPs[ip]; !ok {
			logx.Infof("QoS: IP %s has been removed from Redis blacklist, re-activating locally", ip)
			tm.ipBlacklist.Delete(ip)
			tm.ipPool.ActivateIP(ip)
		}
		return true
	})

	// 更新本地新增的黑名单 IP
	for _, ip := range ips {
		if _, ok := tm.ipBlacklist.Load(ip); !ok {
			tm.ipBlacklist.Store(ip, struct{}{})
			tm.ipPool.DeactivateIP(ip)
		}
	}
}

func (tm *TunnelManager) addTunnel(t *Tunnel) {
	// tunnels contain blacklisted node
	tm.tunnels.Store(t.opts.Id, t)
	tm.ipPool.AddTunnel(t, t.opts.IsBlacklisted)

	// Prometheus 指标：增加活跃隧道数
	metrics.ActiveTunnels.WithLabelValues(tm.config.GetNodeID()).Inc()
}

// 删除 tunnel
func (tm *TunnelManager) removeTunnel(tun *Tunnel) {
	// Only delete from tm.tunnels if this specific tunnel is the one currently stored.
	// This prevents a late-finishing older connection from deleting a newer one.
	if v, ok := tm.tunnels.Load(tun.opts.Id); ok && v == tun {
		tm.tunnels.Delete(tun.opts.Id)
	}

	tm.ipPool.RemoveTunnel(tun)

	// Prometheus 指标：减少活跃隧道数
	metrics.ActiveTunnels.WithLabelValues(tm.config.GetNodeID()).Dec()
}

func (tm *TunnelManager) getShardIndex(nodeID string) uint32 {
	// O(1) tail-sampling optimized for UUID-based IDs
	n := len(nodeID)
	if n >= 4 {
		return (uint32(nodeID[n-1]) |
			uint32(nodeID[n-2])<<8 |
			uint32(nodeID[n-3])<<16 |
			uint32(nodeID[n-4])<<24) % acceptLockShards
	}
	if n > 0 {
		return uint32(nodeID[n-1]) % acceptLockShards
	}
	return 0
}

// BlacklistNode 将节点拉黑 (异步写 Redis 和审计日志)
func (tm *TunnelManager) BlacklistNode(ip string, nodeID string, reason string) {
	logx.Errorf("QoS: Blacklisting node %s (IP: %s), Reason: %s", nodeID, ip, reason)

	// 异步更新 Redis 黑名单
	go func() {

		if err := model.AddBlacklist(tm.redis, []string{ip}); err != nil {
			logx.Errorf("QoS: Failed to add IP %s to Redis blacklist: %v", ip, err)
		}

		// 如果是带宽原因触发的黑名单，设置一个“自动假释期” (TTL)
		// 这样即便 py_check 不跑，节点也会在过期后自动尝试返回
		if strings.Contains(reason, "bandwidth") || strings.Contains(reason, "Strike") || strings.Contains(reason, "Circuit") {
			probationSeconds := tm.config.QoS.ProbationDurationSec
			if probationSeconds <= 0 {
				probationSeconds = 3600 // 默认 60 分钟
			}
			expireTime := time.Now().Unix() + probationSeconds
			if err := model.AddProbation(tm.redis, ip, expireTime); err != nil {
				logx.Errorf("QoS: Failed to add IP %s to probation set: %v", ip, err)
			}
			logx.Infof("QoS: IP %s set with %d-second probation period", ip, probationSeconds)
		}

		// 记录审计日志
		if err := model.AddBlacklistAudit(tm.redis, nodeID, ip, "BLACKLIST", reason); err != nil {
			logx.Errorf("QoS: Failed to add blacklist audit: %v", err)
		}
	}()

	// 更新本地缓存和活跃池
	tm.ipBlacklist.Store(ip, struct{}{})
	tm.ipPool.DeactivateIP(ip)

	// 节点被拉黑后不再主动断开连接，仅仅是在分配层面隔离
	// tm.KickNode(nodeID)
}

// RemoveBlacklistNodes 将多个节点移出黑名单
func (tm *TunnelManager) RemoveBlacklistNodes(ips []string, reason string) {
	logx.Infof("QoS: Removing IPs from blacklist: %v, Reason: %s", ips, reason)

	go func() {
		if err := model.RemoveBlacklist(tm.redis, ips); err != nil {
			logx.Errorf("QoS: Failed to remove IPs from Redis blacklist: %v", err)
		}
		for _, ip := range ips {
			model.AddBlacklistAudit(tm.redis, "multiple", ip, "REMOVE", reason)
		}
	}()

	for _, ip := range ips {
		tm.ipBlacklist.Delete(ip)
		tm.ipPool.ActivateIP(ip)
	}
}

// AddStrike 增加节点的 Strike 计数 (异步)
func (tm *TunnelManager) AddStrike(nodeID string, ip string, reason string) {
	go func() {
		strike, err := model.AddNodeStrike(tm.redis, nodeID)
		if err != nil {
			logx.Errorf("QoS: Failed to add strike for node %s: %v", nodeID, err)
			return
		}

		logx.Infof("QoS: Node %s (IP: %s) strike added: %d/3, Reason: %s", nodeID, ip, strike, reason)

		if strike >= int(tm.config.QoS.StrikeLimit) {
			if tm.config.QoS.EnableBandwidthBlacklist {
				tm.BlacklistNode(ip, nodeID, fmt.Sprintf("Strike limit reached (%d): %s", strike, reason))
			} else {
				logx.Infof("QoS: node %s (IP: %s) reached strike limit (%d), but automatic blacklist is disabled", nodeID, ip, strike)
			}
		}
	}()
}

func (tm *TunnelManager) ClearStrike(nodeID string) {
	go model.ClearNodeStrike(tm.redis, nodeID)
}

func (tm *TunnelManager) acceptWebsocket(conn *websocket.Conn, req *NodeWSReq, nodeIP string) {
	lockIdx := tm.getShardIndex(req.NodeId)

	tm.acceptLocks[lockIdx].Lock()
	unlocked := false
	defer func() {
		if !unlocked {
			tm.acceptLocks[lockIdx].Unlock()
		}
	}()

	v, ok := tm.tunnels.Load(req.NodeId)
	if ok {
		oldTun := v.(*Tunnel)
		logx.Infof("TunnelManager.acceptWebsocket force close tunnel %s ip %s", req.NodeId, nodeIP)
		oldTun.waitClose()
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

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

	localIP := ""
	if addr, _, err := net.SplitHostPort(conn.LocalAddr().String()); err == nil {
		localIP = addr
	} else {
		localIP = conn.LocalAddr().String()
	}

	config := tm.config

	localIP := ""
	if addr, _, err := net.SplitHostPort(conn.LocalAddr().String()); err == nil {
		localIP = addr
	} else {
		localIP = conn.LocalAddr().String()
	}

	opts := &TunOptions{
		Id:                node.Id,
		OS:                node.OS,
		IP:                node.IP,
		LocalIP:           localIP,
		Version:           node.Version,
		UDPTimeout:        int(config.Socks5.UDPTimeout),
		TCPTimeout:        int(config.Socks5.TCPTimeout),
		DownloadRateLimti: config.WS.DownloadRateLimit,
		UploadRateLimit:   config.WS.UploadRateLimit,
		IsBlacklisted:     node.IsBlacklisted,
		LocalIP:           localIP,
	}

	tun := newTunnel(conn, tm, opts, ctx)
	// QoS: 从 Redis 加载节点的 Strike 历史计数，实现状态继承
	if tm.config.QoS.EnableBandwidthBlacklist {
		if strike, err := model.GetNodeStrike(tm.redis, req.NodeId); err == nil {
			tun.strikeCount.Store(uint32(strike))
		}
	}
	tm.addTunnel(tun)

	// Unlock here since the tunnel is successfully added to tm.tunnels
	// Subsequent connections for the same NodeID will now see this new tunnel.
	tm.acceptLocks[lockIdx].Unlock()
	unlocked = true

	// tm.HealthStatsMap.LoadOrStore(node.Id, &HealthStats{})

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

// NodeSource interface implementation, helper methods, and RPC interface implementations
// have been moved to tunmgr_alloc.go and tunmgr_rpc.go respectively.

func (tm *TunnelManager) HandleSocks5TCP(tcpConn *net.TCPConn, targetInfo *socks5.SocksTargetInfo) (err error) {
	logx.Debugf("HandleSocks5TCP, user %s, DomainName %s, port %d, remote:%s, connCount:%d, connTime:%d",
		targetInfo.Username, targetInfo.DomainName, targetInfo.Port, tcpConn.RemoteAddr().String(), tm.socks5ConnCount.Load(), time.Since(targetInfo.ConnCreateTime).Milliseconds())

	tm.socks5ConnCount.Add(1)
	defer tm.socks5ConnCount.Add(-1)

	// 异步上报会话开始，周期与 socks5ConnCount 完全一致
	if tm.perfCollector != nil {
		tm.perfCollector.ReportSessionStart(targetInfo.Username)
		defer tm.perfCollector.ReportSessionEnd(targetInfo.Username)
	}

	if tm.filterRules.isDeny(targetInfo.DomainName, fmt.Sprintf("%d", targetInfo.Port)) {
		tm.reportSOCKS5Error("policy_deny")
		return fmt.Errorf("tcp: target %s:%d have been deny", targetInfo.DomainName, targetInfo.Port)
	}

	user, err := tm.getUserFromCache(targetInfo.Username)
	if err != nil {
		tm.reportSOCKS5Error("internal_error")
		return err
	}

	mode := model.RouteMode(user.RouteMode)
	allocator := tm.allocatorRegistry.Get(mode)
	if allocator == nil {
		tm.reportSOCKS5Error("invalid_mode")
		return fmt.Errorf("no allocator for mode %v", mode)
	}

	tun, userSession, err := allocator.Allocate(user, targetInfo)
	if err != nil {
		tm.reportSOCKS5Error("allocate_fail")
		return err
	}

	if tun == nil {
		tm.reportSOCKS5Error("no_node_available")
		return fmt.Errorf("can not allocate tunnel, user %s", targetInfo.Username)
	}

	logx.Infof("HandleSocks5TCP: user %s session [%s] allocated on node %s, target %s:%d", targetInfo.Username, targetInfo.Session, tun.opts.Id, targetInfo.DomainName, targetInfo.Port)

	if userSession != nil {
		defer tm.sessionManager.Decrement(userSession)
	}

	err = tun.acceptSocks5TCPConn(tcpConn, targetInfo)
	if err != nil {
		if userSession != nil {
			userSession.ReportError()
		}
	} else if userSession != nil {
		userSession.ResetError()
	}
	return err
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

func (tm *TunnelManager) reportSOCKS5Error(errorType string) {
	if tm.perfCollector != nil {
		tm.perfCollector.ReportSOCKS5Error(errorType)
	}
}

func (tm *TunnelManager) reportAuthFailure(userName string) {
	if tm.perfCollector != nil {
		tm.perfCollector.ReportAuthFailure(userName)
	}
}

func (tm *TunnelManager) HandleUserAuth(userName, password string) error {
	logx.Debugf("HandleUserAuth username %s", userName)
	user, err := tm.getUserFromCache(userName)
	if err != nil {
		return fmt.Errorf("get user from redis error %v", err)
	}

	if user == nil {
		tm.reportAuthFailure(userName)
		return fmt.Errorf("user %s not exist", userName)
	}

	if user.Off {
		tm.reportAuthFailure(userName)
		return fmt.Errorf("user %s off", userName)
	}

	hash := md5.Sum([]byte(password))
	passwordMD5 := hex.EncodeToString(hash[:])
	if user.PasswordMD5 != passwordMD5 {
		tm.reportAuthFailure(userName)
		return fmt.Errorf("password not match")
	}

	now := time.Now().Unix()
	if now < user.StartTime || now > user.EndTime {
		tm.reportAuthFailure(userName)
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
			stats := tm.ipPool.GetPoolStats()

			lineInfo := ""
			for line, nodes := range stats.LineNodes {
				lineInfo += fmt.Sprintf("%s:%d ", line, nodes)
			}

			logx.Infof("TunnelManager.keepalive lines:[ %s], tunnel count:%d/%d, cost:%v, ipCount:%d, freeCount:%d, blackCount:%d, assignedCount:%d, session len:%d",
				lineInfo, count, stats.TunnelCount, time.Since(now), stats.TotalIPCount, stats.FreeIPCount, stats.BlacklistIPCount, stats.AssignedIPCount, tm.sessionManager.SessionLen())
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

		// if err := model.AddUsersTrafficOneHour(context.TODO(), tm.redis, trafficMap); err != nil {
		// 	logx.Errorf("AddUsersDayTraffic failed:%v", err)
		// }

		// if err := model.AddUsersTraffic5Minutes(context.TODO(), tm.redis, trafficMap); err != nil {
		// 	logx.Errorf("AddUsersDayTraffic failed:%v", err)
		// }

		if err := model.AddUsersTotalTraffic(context.TODO(), tm.redis, trafficMap); err != nil {
			logx.Errorf("AddUsersTotalTraffic failed:%v", err)
		}

	}
}

func (tm *TunnelManager) addUserTrafficStats(user string, downloadTraffic int64, uploadTraffic int64) {
	if tm.config.TrafficStats.EnableUserTraffic {
		tm.userTraffic.add(user, downloadTraffic+uploadTraffic)
	}
}
