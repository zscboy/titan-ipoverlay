package logic

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync/atomic"
	"time"

	"titan-ipoverlay/manager/internal/svc"
	"titan-ipoverlay/manager/internal/types"
	"titan-ipoverlay/manager/model"

	"github.com/golang-jwt/jwt/v4"
	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/stores/redis"
)

const (
	NodeAccessPointDefaultKey = "Default"
	StrategyNameRegion        = "region"
	StrategyNameVendor        = "vendor"
)

type Location struct {
	Country  string `json:"country"`
	Province string `json:"province"`
	City     string `json:"city"`
	IP       string `json:"ip"`
}

//	type LocationData struct {
//		Location *Location `json:"location"`
//	}
type LocationResp struct {
	Code int       `json:"code"`
	Data *Location `json:"data"`
	Msg  string    `json:"msg"`
}

type GetNodePopLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewGetNodePopLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetNodePopLogic {
	return &GetNodePopLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetNodePopLogic) GetNodePop(req *types.GetNodePopReq) (resp *types.GetNodePopResp, err error) {
	popEntity, err := l.allocatePop(req)
	if err != nil {
		return nil, err
	}

	tokenBytes, err := l.generateJwtToken(popEntity.AccessSecret, popEntity.AccessExpire, req.NodeId)
	if err != nil {
		return nil, err
	}

	logx.Debugf("GetNodePop, %s accessPoint %s, getTokenResp:%s", req.NodeId, popEntity.Config.WSURL, string(tokenBytes))
	return &types.GetNodePopResp{ServerURL: popEntity.Config.WSURL, AccessToken: string(tokenBytes)}, nil
}

func (l *GetNodePopLogic) allocatePop(req *types.GetNodePopReq) (*svc.Pop, error) {
	ipValue := l.ctx.Value("Remote-IP")
	ip := ipValue.(string)
	if len(ip) == 0 {
		return nil, fmt.Errorf("can not get remote ip")
	}

	// 1. Check Blacklist Priority (P1)
	_, isBlacklisted := l.svcCtx.BlacklistMap.Load(ip)

	if isBlacklisted {
		blacklistPopID := l.svcCtx.Config.Strategy.BlacklistPopId
		logx.Infof("allocatePop node %s ip %s is in blacklist, redirect to blacklist pop %s", req.NodeId, ip, blacklistPopID)
		if pop, ok := l.svcCtx.Pops[blacklistPopID]; ok {
			return pop, nil
		}
	}

	// 2. Sticky allocation (keep existing pop if IP hasn't changed or matches Vendor Strategy)
	popID, nodeIP, err := l.getNodePopIP(req.NodeId)
	if err != nil {
		logx.Errorf("GetNodePopIP error: %v, node %s ip %s", err, req.NodeId, ip)
		return nil, err
	}

	if len(popID) > 0 {
		pop, exists := l.svcCtx.Pops[popID]

		// Case A: IP matches perfectly
		if ip == nodeIP {
			if exists {
				logx.Debugf("allocatePop node %s ip %s sticky to existing pop %s", req.NodeId, nodeIP, popID)
				return pop, nil
			}
			logx.Errorf("allocatePop node %s ip %s sticky pop %s not found in memory", req.NodeId, nodeIP, popID)
		} else {
			// Case B: IP changed, check if we should keep it anyway (Vendor Strategy)
			_, hasVendorStrategy := l.svcCtx.VendorStrategy[req.Vendor]
			if hasVendorStrategy {
				if exists {
					logx.Infof("allocatePop node %s vendor %s strategy: keep pop %s despite ip change (%s -> %s)", req.NodeId, req.Vendor, popID, nodeIP, ip)
					return pop, nil
				}
				logx.Errorf("allocatePop node %s vendor strategy: sticky pop %s not found", req.NodeId, popID)
			} else {
				// Case C: IP changed and no Vendor Strategy, need to re-allocate
				logx.Debugf("allocatePop node %s ip changed (%s -> %s), will re-allocate from pop %s", req.NodeId, nodeIP, ip, popID)
			}
		}
	} else {
		logx.Debugf("allocatePop node %s ip %s not found in memory, will allocate", req.NodeId, ip)
	}

	// 3. Region Strategy (P2)
	location, err := l.getLocalInfo(ip)
	if err != nil {
		logx.Errorf("GetIPLocation error: %v, node %s, ip %s", err, req.NodeId)
		return nil, err
	}

	if pop, err := l.matchAndAllocatePop(l.svcCtx.RegionStrategy[location.Country], StrategyNameRegion+location.Country, req.NodeId, ip); err != nil {
		logx.Errorf("matchAndAllocatePop region error: %v", err)
		return nil, err
	} else if pop != nil {
		return pop, nil
	}

	// 4. Vendor Strategy (P3)
	if pop, err := l.matchAndAllocatePop(l.svcCtx.VendorStrategy[req.Vendor], StrategyNameVendor+req.Vendor, req.NodeId, ip); err != nil {
		logx.Errorf("matchAndAllocatePop vendor error: %v", err)
		return nil, err
	} else if pop != nil {
		return pop, nil
	}

	// 5. Default Strategy (P4)
	defaultPopID := l.svcCtx.Config.Strategy.DefaultPopId
	if pop, ok := l.svcCtx.Pops[defaultPopID]; ok {
		logx.Debugf("node %s allocate default pop:%s", req.NodeId, defaultPopID)
		l.saveNodePop(req.NodeId, defaultPopID, ip)
		return pop, nil
	}

	return nil, fmt.Errorf("no pop found for %s, location:%v, vendor:%s", req.NodeId, location, req.Vendor)
}

func (l *GetNodePopLogic) matchAndAllocatePop(popIds []string, strategyName, nodeID, ip string) (*svc.Pop, error) {
	if len(popIds) == 0 {
		return nil, nil
	}

	pop, err := l.selectPopFromList(l.ctx, strategyName, popIds)
	if err != nil {
		return nil, err
	}

	if pop != nil {
		logx.Debugf("node %s matched %s, allocate pop:%s", nodeID, strategyName, pop.Config.Id)
		l.saveNodePop(nodeID, pop.Config.Id, ip)
		return pop, nil
	}

	return nil, fmt.Errorf("no pop found for %s, strategy: %s", nodeID, strategyName)
}

func (l *GetNodePopLogic) saveNodePop(nodeID, popID, ip string) {
	if err := model.SetNodePopIP(l.svcCtx.Redis, nodeID, popID, ip); err != nil {
		logx.Errorf("allocatePop SetNodePop error %v", err)
		return
	}
	l.svcCtx.NodePopCache.Store(nodeID, &svc.NodeCacheItem{PopID: popID, IP: ip})
}

func (l *GetNodePopLogic) getNodePopIP(nodeID string) (string, string, error) {
	if val, ok := l.svcCtx.NodePopCache.Load(nodeID); ok {
		if item, ok := val.(*svc.NodeCacheItem); ok {
			return item.PopID, item.IP, nil
		}
	}

	pID, nIP, err := model.GetNodePopIP(l.svcCtx.Redis, nodeID)
	if err != nil {
		return "", "", err
	}

	popID := string(pID)
	nodeIP := string(nIP)
	if len(popID) > 0 {
		l.svcCtx.NodePopCache.Store(nodeID, &svc.NodeCacheItem{PopID: popID, IP: nodeIP})
	}
	return popID, nodeIP, nil
}

func (l *GetNodePopLogic) selectPopFromList(ctx context.Context, strategyName string, popIds []string) (*svc.Pop, error) {
	if len(popIds) == 0 {
		return nil, nil
	}

	// 获取或初始化该策略的计数器
	val, _ := l.svcCtx.StrategyRR.LoadOrStore(strategyName, new(uint64))
	counter := val.(*uint64)

	// 使用原子操作增加该策略的计数器
	idx := atomic.AddUint64(counter, 1)
	n := uint64(len(popIds))

	// 尝试轮询列表中的每一个 ID
	for i := uint64(0); i < n; i++ {
		targetIdx := (idx + i) % n
		id := popIds[targetIdx]

		if popEntity, ok := l.svcCtx.Pops[id]; ok {
			return popEntity, nil
		}
	}

	return nil, fmt.Errorf("selectPopFromList no pop found for %s, strategy: %s", strategyName, popIds)
}

func (l *GetNodePopLogic) getLocalInfo(ip string) (*Location, error) {
	loc, err := model.GetIPLocation(l.svcCtx.Redis, ip)
	if err == nil {
		return &Location{IP: loc.IP, City: loc.City, Province: loc.Province, Country: loc.Country}, nil
	} else if err != redis.Nil {
		logx.Errorf("GetIPLocation error: %v", err)
	}

	v, err := l.svcCtx.IPGroup.Do(ip, func() (interface{}, error) {

		location, err := l.httpGetLocationInfo(ip)
		if err != nil {
			return nil, err
		}

		redisIPLocation := model.IPLocation{IP: location.IP, City: location.City, Province: location.Province, Country: location.Country}
		if err := model.SaveIPLocation(l.svcCtx.Redis, &redisIPLocation); err != nil {
			logx.Errorf("SaveIPLocation:%v", err)
		}

		return location, nil
	})

	if err != nil {
		return nil, err
	}

	return v.(*Location), nil
}

func (l *GetNodePopLogic) httpGetLocationInfo(ip string) (*Location, error) {
	logx.Infof("get %s local info from %s", ip, l.svcCtx.Config.GeoAPI.API)

	url := fmt.Sprintf("%s?ip=%s", l.svcCtx.Config.GeoAPI.API, ip)

	client := &http.Client{
		Timeout: 3 * time.Second,
	}

	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bs, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("StatusCode %d, msg:%s", resp.StatusCode, string(bs))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	locationResp := &LocationResp{}
	err = json.Unmarshal(body, locationResp)
	if err != nil {
		return nil, err
	}

	if locationResp.Code != 0 && locationResp.Code != 200 {
		return nil, fmt.Errorf("code:%d, msg:%s", locationResp.Code, locationResp.Msg)
	}

	if locationResp.Data == nil || len(locationResp.Data.Country) == 0 {
		return nil, fmt.Errorf("resp:%s", string(body))
	}

	location := locationResp.Data

	return location, nil
}

func (l *GetNodePopLogic) generateJwtToken(secret string, expire int64, nodeId string) ([]byte, error) {
	claims := jwt.MapClaims{
		"user": nodeId,
		"exp":  time.Now().Add(time.Second * time.Duration(expire)).Unix(),
		"iat":  time.Now().Add(-5 * time.Minute).Unix(),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenStr, err := token.SignedString([]byte(secret))
	if err != nil {
		return nil, err
	}

	return []byte(tokenStr), nil
}
