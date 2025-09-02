package ws

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"
	"titan-ipoverlay/ippop/api/internal/config"
	"titan-ipoverlay/ippop/api/model"
	"titan-ipoverlay/ippop/api/socks5"

	"github.com/bluele/gcache"
	"github.com/gorilla/websocket"
	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/stores/redis"
	"golang.org/x/time/rate"
)

const (
	userCacheSize = 512
	// 30 seconds
	keepaliveInterval       = 30
	userTrafficSaveInterval = 300
	onlineTableExpireTime   = 6 * keepaliveInterval
	// userCacheExpire = 60
)

type TunnelManager struct {
	tunnels sync.Map
	// svcCtx  *svc.ServiceContext
	redis       *redis.Redis
	config      config.Config
	userTraffic *userTraffic
	userCache   gcache.Cache
	userTunLock sync.Mutex
}

func NewTunnelManager(config config.Config, redis *redis.Redis) *TunnelManager {
	if err := model.DeleteNodeOnlineData(redis); err != nil {
		panic(err)
	}

	tm := &TunnelManager{
		config:      config,
		redis:       redis,
		userTraffic: newUserTraffic(),
		userCache:   gcache.New(userCacheSize).LRU().Build(),
		userTunLock: sync.Mutex{},
	}
	go tm.keepalive()
	go tm.setNodeOnlineDataExpire()
	go tm.startUserTrafficTimer()
	return tm
}

func (tm *TunnelManager) acceptWebsocket(conn *websocket.Conn, node *model.Node) {
	v, ok := tm.tunnels.Load(node.Id)
	if ok {
		oldTun := v.(*Tunnel)
		oldTun.close()
	}

	logx.Debugf("TunnelManager:%s accept websocket ", node.Id)

	socksConfig := tm.config.Socks5
	opts := &TunOptions{
		Id:         node.Id,
		OS:         node.OS,
		IP:         node.IP,
		UDPTimeout: int(socksConfig.UDPTimeout),
		TCPTimeout: int(socksConfig.TCPTimeout),
	}

	tun := newTunnel(conn, tm, opts)
	defer tun.leaseComplete()

	tm.tunnels.Store(node.Id, tun)
	defer tm.tunnels.Delete(node.Id)

	if err := model.SetNodeAndZadd(context.Background(), tm.redis, node); err != nil {
		logx.Errorf("SetNode failed:%s", err.Error())
		return
	}

	if err := model.SetNodeOnline(tm.redis, node.Id); err != nil {
		logx.Errorf("SetNodeOnline failed:%s", err.Error())
		return
	}

	if len(node.BindUser) == 0 {
		model.AddFreeNode(tm.redis, node.Id)
	}

	defer tm.handleNodeOffline(node.Id)

	tun.serve()
}

func (tm *TunnelManager) handleNodeOffline(nodeID string) {
	if err := model.SetNodeOffline(tm.redis, nodeID); err != nil {
		logx.Errorf("handleNodeOffline SetNodeOffline %v", err)
	}

	node, err := model.GetNode(tm.redis, nodeID)
	if err != nil {
		logx.Errorf("handleNodeOffline GetNode %v", err)
		return
	}

	if node == nil {
		logx.Errorf("handleNodeOffline node == nil, id:%s", nodeID)
		return
	}

	if len(node.BindUser) > 0 {
		logx.Debugf("node %s offline, user %s trigger siwth node", node.Id, node.BindUser)
		if err := tm.swithNodeForUser(node.BindUser); err != nil {
			logx.Errorf("handleNodeOffline swithNodeForUser %s failed: %v", node.BindUser, err)
		}
	} else {
		if err := model.RemoveFreeNode(tm.redis, nodeID); err != nil {
			logx.Errorf("handleNodeOffline RemoveFreeNode %v", err)
		}
	}

}

func (tm *TunnelManager) swithNodeForUser(userName string) error {
	user, err := model.GetUser(tm.redis, userName)
	if err != nil {
		return fmt.Errorf("swithNodeForUser  GetUser:%v", err)
	}

	if user == nil {
		return fmt.Errorf("swithNodeForUser user %s not exist", userName)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	newNodeID, err := model.AllocateFreeNode(ctx, tm.redis)
	if err != nil {
		return err
	}

	if err := model.SwitchNodeByUser(ctx, tm.redis, user, string(newNodeID)); err != nil {
		if err2 := model.AddFreeNode(tm.redis, string(newNodeID)); err2 != nil {
			logx.Errorf("swithNodeForUser AddFreeNode: %v", err2)
		}
		return err
	}

	tm.DeleteUserFromCache(userName)

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
	logx.Debugf("getUserFromCache v:%v", v)
	return v.(*model.User), nil
}

func (tm *TunnelManager) DeleteUserFromCache(userName string) {
	tm.userCache.Remove(userName)
}

func (tm *TunnelManager) getTunnelByUser(userName string) (*Tunnel, error) {
	tm.userTunLock.Lock()
	defer tm.userTunLock.Unlock()

	user, err := tm.getUserFromCache(userName)
	if err != nil {
		return nil, err
	}

	tun := tm.getTunnel(user.RouteNodeID)
	if tun == nil {
		return nil, nil
	}

	if user.DownloadRateLimit <= 0 {
		tun.readLimiter = nil
	} else {
		if tun.readLimiter == nil || tun.readLimiter.Limit() != rate.Limit(user.DownloadRateLimit) {
			tun.readLimiter = rate.NewLimiter(rate.Limit(user.DownloadRateLimit), limitRateBurst)
			logx.Debugf("new readLimiter for user:%s", user.UserName)
		}
	}

	if user.UploadRateLimit <= 0 {
		tun.writeLimiter = nil
	} else {
		if tun.writeLimiter == nil || tun.writeLimiter.Limit() != rate.Limit(user.UploadRateLimit) {
			tun.writeLimiter = rate.NewLimiter(rate.Limit(user.UploadRateLimit), limitRateBurst)
			logx.Debugf("new writerLimiter for user:%s", user.UserName)
		}
	}

	// logx.Debugf("user download rate limit:%d, upload rate limit:%d, raadLimiter:%f", user.DownloadRateLimit, user.UploadRateLimit, tun.readLimiter.Limit())

	return tun, nil

}

func (tm *TunnelManager) HandleSocks5TCP(tcpConn *net.TCPConn, targetInfo *socks5.SocksTargetInfo) error {
	logx.Debugf("HandleSocks5TCP, user %s, DomainName %s, port %d", targetInfo.UserName, targetInfo.DomainName, targetInfo.Port)
	tun, err := tm.getTunnelByUser(targetInfo.UserName)
	if err != nil {
		return err
	}
	if tun == nil {
		return fmt.Errorf("can not allocate tunnel, user %s", targetInfo.UserName)
	}

	return tun.acceptSocks5TCPConn(tcpConn, targetInfo)
}

func (tm *TunnelManager) HandleSocks5UDP(udpConn socks5.UDPConn, udpInfo *socks5.Socks5UDPInfo, data []byte) error {
	tun, err := tm.getTunnelByUser(udpInfo.UserName)
	if err != nil {
		return err
	}
	if tun == nil {
		return fmt.Errorf("can not allocate tunnel, user %s", udpInfo.UserName)
	}

	return tun.acceptSocks5UDPData(udpConn, udpInfo, data)
}

func (tm *TunnelManager) HandleUserAuth(userName, password string) error {
	logx.Debugf("HandleUserAuth userName %s password %s", userName, password)
	user, err := model.GetUser(tm.redis, userName)
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
			now := time.Now()
			tm.tunnels.Range(func(key, value any) bool {
				t := value.(*Tunnel)
				t.keepalive()
				count++
				return true
			})

			logx.Debugf("TunnelManager.keepalive tunnel count:%d, cost:%v", count, time.Since(now))
		}
	}
}

func (tm *TunnelManager) setNodeOnlineDataExpire() {
	ticker := time.NewTicker(keepaliveInterval * time.Second)
	defer ticker.Stop()

	for {
		<-ticker.C
		model.SetNodeOnlineDataExpire(tm.redis, onlineTableExpireTime)
	}
}

func (tm *TunnelManager) startUserTrafficTimer() {
	ticker := time.NewTicker(userTrafficSaveInterval * time.Second)
	defer ticker.Stop()

	for {
		<-ticker.C
		trafficMap := tm.userTraffic.snapshotAndClear()
		for userName, traffic := range trafficMap {
			if traffic > 0 {
				user, err := model.GetUser(tm.redis, userName)
				if err != nil {
					logx.Errorf("get user %v", err)
					continue
				}

				if user == nil {
					logx.Errorf("user %s not exist", userName)
					continue
				}

				user.CurrentTraffic += traffic
				model.SaveUser(tm.redis, user)
			}
		}
	}
}

func (tm *TunnelManager) getTunnel(id string) *Tunnel {
	v, ok := tm.tunnels.Load(id)
	if !ok {
		return nil
	}
	return v.(*Tunnel)
}

func (tm *TunnelManager) traffic(userName string, traffic int64) {
	tm.userTraffic.add(userName, traffic)
}
